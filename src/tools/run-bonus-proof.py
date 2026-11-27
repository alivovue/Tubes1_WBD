#!/usr/bin/env python3
"""Regenerate M2 evidence; private processes, databases and credentials stay ignored."""
import argparse
import base64
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "output"
parser = argparse.ArgumentParser()
parser.add_argument("--tools-directory", type=Path, required=True, help="Extracted official CodeQL/k6/observability releases")
parser.add_argument("--stage", choices=["all", "codeql", "batch", "telemetry"], default="all")
args = parser.parse_args()
tools = args.tools_directory.resolve()
run_id = dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%SZ")
runtime = ROOT / ".schakker/bonus-proof" / run_id
runtime.mkdir(parents=True, mode=0o700)
temp = Path(tempfile.mkdtemp(prefix="bp-", dir=os.environ.get("SCHAKKER_PROOF_TMPDIR", tempfile.gettempdir())))
environment = dict(os.environ, TMPDIR=str(temp), TMP=str(temp), TEMP=str(temp), GOTOOLCHAIN="local", GOPROXY="off")
environment["JAVA_TOOL_OPTIONS"] = f"-XX:-UsePerfData -Djava.io.tmpdir={temp}"
environment["GOCACHE"] = str(ROOT / ".schakker/history-verification/go-build")
commands = []
processes = []
report = {"run_id": run_id, "requested_stage": args.stage, "started_at_utc": dt.datetime.now(dt.timezone.utc).isoformat(), "stages": {}, "commands": commands}


def now():
    return dt.datetime.now(dt.timezone.utc).isoformat()


def save(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, indent=2, ensure_ascii=False) + "\n")


def tool(name):
    candidates = [p for p in tools.rglob(name) if p.is_file() and os.access(p, os.X_OK)]
    candidates.sort(key=lambda p: (p.parent.name != "bin", len(p.parts), str(p)))
    if not candidates:
        raise RuntimeError(f"Missing official tool: {name}")
    return str(candidates[0])


def command(argv, name, extra=None, allowed=(0,), timeout=1800):
    print(f"[{now()}] {name}", flush=True)
    entry = {"name": name, "argv": [str(v) for v in argv], "started_at_utc": now()}
    commands.append(entry)
    with (runtime / (name + ".log")).open("w") as log:
        process = subprocess.Popen([str(v) for v in argv], cwd=ROOT, env={**environment, **(extra or {})}, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
        try:
            process.wait(timeout=timeout)
        except BaseException:
            # Include compiler, browser and test children when interrupted/timed out.
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                pass
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.wait()
            entry["interrupted_or_timed_out"] = True
            raise
        finally:
            entry.update(exit_code=process.returncode, finished_at_utc=now())
            save(OUTPUT / "run.json", report)
    if process.returncode not in allowed:
        raise RuntimeError(f"{name} exited {process.returncode}; inspect {runtime / (name + '.log')}")
    return process.returncode


def start(argv, name, extra=None):
    log = (runtime / (name + ".log")).open("w")
    process = subprocess.Popen([str(v) for v in argv], cwd=ROOT, env={**environment, **(extra or {})}, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
    processes.append((name, process, log))
    return process


def fetch(url, *, auth=None, text=False, headers=None):
    values = dict(headers or {})
    if auth:
        values["Authorization"] = "Basic " + base64.b64encode(auth.encode()).decode()
    with urllib.request.urlopen(urllib.request.Request(url, headers=values), timeout=15) as response:
        body = response.read().decode()
    return body if text else json.loads(body)


def ready(url, process, timeout=120):
    until = time.monotonic() + timeout
    while time.monotonic() < until:
        if process.poll() is not None:
            raise RuntimeError(f"Service exited before {url} was ready")
        try:
            fetch(url, text=True)
            return
        except (OSError, urllib.error.URLError):
            time.sleep(1)
    raise RuntimeError(f"Timed out waiting for {url}")


def snapshot():
    files = subprocess.check_output(["git", "ls-files", "--cached", "--others", "--exclude-standard", "src", "go.work", "go.work.sum", "package.json", "package-lock.json"], cwd=ROOT, text=True).splitlines()
    hashes = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in sorted(set(files)) if (ROOT / name).is_file()}
    save(OUTPUT / "source-manifest.json", {"recorded_at_utc": now(), "base_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(), "source_sha256": hashes})
    return hashes


def codeql():
    cli = tool("codeql")
    scans = []
    for language, pack in [("go", "go"), ("javascript", "javascript")]:
        database = runtime / ("codeql-" + language)
        extra = {"GOCACHE": str(runtime / "codeql-go-cache")}
        argv = [cli, "database", "create", str(database), "--source-root=" + str(ROOT), "--language=" + language, "--threads=2", "--ram=4096", "--codescanning-config=" + str(ROOT / "src/tools/ci-templates/codeql/config.yml")]
        if language == "go":
            argv.append("--command=bash src/tools/codeql-build.sh")
        else:
            argv.append("--build-mode=none")
        command(argv, "codeql-create-" + language, extra, timeout=2400)
        destination = OUTPUT / "codeql" / (language + ".sarif")
        command([cli, "database", "analyze", str(database), f"codeql/{pack}-queries:codeql-suites/{pack}-security-extended.qls", "--format=sarif-latest", "--output=" + str(destination), "--threads=2", "--ram=4096", "--sarif-category=" + language, "--sarif-add-snippets"], "codeql-analyze-" + language, timeout=2400)
        data = json.loads(destination.read_text())
        for run in data["runs"]:
            for base in run.get("originalUriBaseIds", {}).values():
                if base.get("uri", "").startswith(ROOT.as_uri()):
                    base["uri"] = "file:///workspace/"
        save(destination, data)
        archived = []
        archive_hashes = {}
        source_zip = database / "src.zip"
        if source_zip.exists():
            with zipfile.ZipFile(source_zip) as archive:
                prefix = str(ROOT).lstrip("/") + "/"
                archived = sorted(name.split(prefix, 1)[1] for name in archive.namelist() if prefix in name)
                archive_hashes = {name.split(prefix, 1)[1]: hashlib.sha256(archive.read(name)).hexdigest() for name in archive.namelist() if prefix in name and not name.endswith("/")}
        extensions = {".go"} if language == "go" else {".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"}
        expected = {name for name in json.loads((OUTPUT / "source-manifest.json").read_text())["source_sha256"] if Path(name).suffix in extensions}
        analyzed = {name for name in archive_hashes if Path(name).suffix in extensions}
        matches = all((ROOT / name).is_file() and hashlib.sha256((ROOT / name).read_bytes()).hexdigest() == digest for name, digest in archive_hashes.items())
        if not matches:
            raise RuntimeError("Source changed after CodeQL extraction; rerun the scan before publishing evidence")
        scans.append({"language": language, "sarif": "output/codeql/" + destination.name, "sha256": hashlib.sha256(destination.read_bytes()).hexdigest(), "findings": sum(len(run.get("results", [])) for run in data["runs"]), "extracted_sources": archived, "archived_source_sha256": archive_hashes, "current_source_matches_archive": matches, "language_source_count": len(analyzed), "expected_language_sources": len(expected), "excluded_language_sources": sorted(expected - analyzed), "execution_successful": all(inv.get("executionSuccessful", True) for run in data["runs"] for inv in run.get("invocations", []))})
    workflow = ".github/workflows/codeql.yml"
    save(OUTPUT / "codeql/manifest.json", {"generated_at_utc": now(), "query_suite": "security-extended", "fresh_databases": True, "execution": "local CLI", "github_actions_workflow": workflow, "github_actions_workflow_configured": (ROOT / workflow).is_file(), "scans": scans})
    command([sys.executable, "src/tools/codeql-report.py", "output/codeql/go.sarif", "output/codeql/javascript.sarif", "--output", "output/codeql/review.json"], "codeql-inventory")
    report["stages"]["codeql"] = {"status": "scanned; findings require review", "scans": [{k: v for k, v in scan.items() if k not in {"extracted_sources", "archived_source_sha256", "excluded_language_sources"}} for scan in scans]}


def batch():
    code = command(["go", "-C", "src/tests", "test", "-count=1", "-timeout=30m", "-v", "-run", "^TestBatchPerformanceEvidence$", "./integration"], "batch-benchmark", {"SCHAKKER_RUN_BATCH_BENCHMARK": "1", "SCHAKKER_BATCH_REPORT": str(OUTPUT / "batch/benchmark.json")}, timeout=1900)
    save(OUTPUT / "batch/run.json", {"completed_at_utc": now(), "exit_code": code, "test": "TestBatchPerformanceEvidence", "fresh_dataset": True})
    report["stages"]["batch"] = {"status": "passed", "result": "output/batch/benchmark.json"}


def telemetry():
    for port in [3030, 3100, 3200, 3300, 4317, 4318, 4319, 9090, 9095, 9096, 9468, 13133]:
        with socket.socket() as probe:
            if probe.connect_ex(("127.0.0.1", port)) == 0:
                raise RuntimeError(f"Proof port {port} is occupied; existing services were left untouched")
    stack_data = runtime / "observability"
    stack = start([sys.executable, "src/ops/observability/run-local.py", "--tools-directory", str(tools), "--data-directory", str(stack_data), "--metrics-target", "127.0.0.1:9468"], "observability-stack")
    for url in ["http://127.0.0.1:3030/api/health", "http://127.0.0.1:9090/-/ready", "http://127.0.0.1:3100/ready", "http://127.0.0.1:3200/ready", "http://127.0.0.1:13133"]:
        ready(url, stack)
    command(["go", "-C", "src/tools", "build", "-o", str(runtime / "loadfixture"), "./cmd/loadfixture"], "build-loadfixture", {"GOWORK": "off"})
    manifest = runtime / "fixture.json"
    otlp = {"OTEL_EXPORTER_OTLP_ENDPOINT": "http://127.0.0.1:4318", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "", "OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "", "SCHAKKER_METRICS_ADDR": "127.0.0.1:9468"}
    fixture = start([runtime / "loadfixture", "-manifest", manifest, "-listen", "127.0.0.1:3300"], "fixture", otlp)
    ready("http://127.0.0.1:3300/fixture/health", fixture)
    # Only the summary is public; the input manifest contains disposable secrets.
    code = command([tool("k6"), "run", "src/tests/k6/stress.js"], "k6-stress", {"SCHAKKER_K6_FIXTURE": str(manifest), "SCHAKKER_K6_SUMMARY": str(OUTPUT / "k6/summary.json")}, allowed=(0, 99), timeout=300)
    result = json.loads((OUTPUT / "k6/summary.json").read_text())
    rendered = subprocess.check_output([sys.executable, "src/tests/k6/report.py", "output/k6/summary.json"], cwd=ROOT, text=True)
    (OUTPUT / "k6/results.md").write_text(rendered)
    failures = {name: threshold for name, metric in result["metrics"].items() for threshold, state in metric.get("thresholds", {}).items() if not state["ok"]}
    save(OUTPUT / "k6/run.json", {"completed_at_utc": now(), "exit_code": code, "failed_thresholds": failures, "fresh_targets": 3, "worker_concurrency": 4, "thresholds_not_relaxed": True})
    print(f"[{now()}] k6 finished (exit {code}); collecting live telemetry", flush=True)
    report["stages"]["k6"] = {"status": "passed" if code == 0 else "threshold failure retained", "failed_thresholds": failures}
    time.sleep(8)
    parameters = urllib.parse.urlencode({"query": 'schakker_migration_duration_seconds_bucket', "start": str(time.time() - 600), "end": str(time.time())})
    exemplars = fetch("http://127.0.0.1:9090/api/v1/query_exemplars?" + parameters)
    query = urllib.parse.urlencode({"query": '{service_name="schakker-load-fixture"} |= "worker.finished"', "limit": 1000, "start": str(int((time.time() - 600) * 1e9)), "end": str(int(time.time() * 1e9))})
    logs = fetch("http://127.0.0.1:3100/loki/api/v1/query_range?" + query)
    exemplar_traces = {e.get("labels", {}).get("trace_id") for series in exemplars.get("data", []) for e in series.get("exemplars", [])}
    matching = []
    for stream in logs.get("data", {}).get("result", []):
        for value in stream.get("values", []):
            # Loki may project structured metadata into the response stream map.
            metadata = {**stream.get("stream", {}), **(value[2] if len(value) > 2 else {})}
            trace_id = metadata.get("trace_id")
            if trace_id in exemplar_traces:
                matching.append((trace_id, value, metadata))
    if not matching:
        raise RuntimeError("No worker log and Prometheus exemplar share a trace ID")
    trace_id, log, metadata = matching[0]
    trace = fetch("http://127.0.0.1:3200/api/traces/" + trace_id, headers={"Accept": "application/json"})
    spans = []
    def walk(value):
        if isinstance(value, dict):
            if "spanId" in value and "name" in value:
                spans.append(value)
            for item in value.values(): walk(item)
        elif isinstance(value, list):
            for item in value: walk(item)
    walk(trace)
    if "worker.execute" not in {span["name"] for span in spans}:
        raise RuntimeError("Correlated trace has no worker span")
    password = (stack_data / "grafana_password").read_text().strip()
    dashboard = fetch("http://127.0.0.1:3030/api/dashboards/uid/schakker-migrations", auth="admin:" + password)
    metrics = {"exemplars": exemplars, "queries": {}}
    for expression in ["schakker_jobs", "schakker_database_pool_connections", "schakker_database_pool_wait_seconds_total", "schakker_database_pool_wait_total", "schakker_migration_rows", "schakker_database_lock_wait_seconds_count"]:
        metrics["queries"][expression] = fetch("http://127.0.0.1:9090/api/v1/query?" + urllib.parse.urlencode({"query": expression}))
    raw_metrics = fetch("http://127.0.0.1:9468/metrics", text=True, headers={"Accept": "application/openmetrics-text"})
    (OUTPUT / "observability/metrics.prom").write_text("\n".join(line for line in raw_metrics.splitlines() if line.startswith(("schakker_", "# HELP schakker_", "# TYPE schakker_"))) + "\n")
    for name, data in [("trace", trace), ("logs", logs), ("metrics", metrics), ("dashboard", dashboard)]:
        save(OUTPUT / "observability" / (name + ".json"), data)
    save(OUTPUT / "observability/config.json", {"metrics_target": "127.0.0.1:9468", "prometheus_config": (stack_data / "prometheus.yml").read_text(), "tracked_configuration": "src/ops/observability", "fresh_storage": True})
    command(["node", "src/tools/capture-grafana.mjs", stack_data / "grafana_password", OUTPUT / "observability/grafana.png"], "grafana-screenshot", timeout=150)
    save(OUTPUT / "observability/verification.json", {"verified_at_utc": now(), "status": "passed", "trace_id": trace_id, "span_count": len(spans), "span_names": sorted({span["name"] for span in spans}), "matching_worker_log": {"timestamp_ns": log[0], "body": log[1], "attributes": metadata, "attributes_source": "Loki query response, including projected structured metadata"}, "dashboard_uid": dashboard["dashboard"]["uid"], "dashboard_panels": len(dashboard["dashboard"].get("panels", [])), "prometheus_scrapes_only_fresh_fixture": True, "screenshot": "output/observability/grafana.png"})
    report["stages"]["observability"] = {"status": "passed", "trace_id": trace_id}
    # Prove that no fixture credential was exported into the public artifacts.
    private = json.loads(manifest.read_text())
    secrets = [v for k, v in private.items() if isinstance(v, str) and any(word in k.lower() for word in ["password", "token", "cookie"]) and len(v) > 10]
    for path in OUTPUT.rglob("*"):
        if path.is_file() and path.suffix != ".png":
            raw = path.read_text()
            if any(secret in raw for secret in secrets): raise RuntimeError(f"Private fixture value leaked into {path}")


def cleanup():
    stopped = []
    for name, process, log in reversed(processes):
        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=45)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait(timeout=10)
        stopped.append({"service": name, "exit_code": process.returncode})
        log.close()
    # A hard-killed test process cannot run its own finally. Stop only clusters
    # inside this run's private temporary directory, never development databases.
    pg_ctl = shutil.which("pg_ctl")
    if not pg_ctl:
        matches = sorted(Path("/usr/lib/postgresql").glob("*/bin/pg_ctl"))
        if matches: pg_ctl = str(matches[-1])
    remaining = []
    for pid_file in temp.rglob("postmaster.pid"):
        if pg_ctl:
            subprocess.run([pg_ctl, "-D", str(pid_file.parent), "-m", "fast", "-w", "-t", "15", "stop"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=20)
        if pid_file.exists(): remaining.append(str(pid_file.relative_to(temp)))
    report["cleanup"] = {"processes": stopped, "remaining_test_clusters": remaining}
    if not remaining: shutil.rmtree(temp, ignore_errors=True)


for directory in ["codeql", "observability", "k6", "batch"]:
    (OUTPUT / directory).mkdir(parents=True, exist_ok=True)
proof_files = {
    "codeql": ["codeql/go.sarif", "codeql/javascript.sarif", "codeql/manifest.json", "codeql/review.json"],
    "batch": ["batch/benchmark.json", "batch/run.json"],
    "telemetry": ["k6/summary.json", "k6/results.md", "k6/run.json", "observability/verification.json", "observability/trace.json", "observability/logs.json", "observability/metrics.prom", "observability/metrics.json", "observability/dashboard.json", "observability/grafana.png", "observability/config.json"],
}
for stage, names in proof_files.items():
    if args.stage in ["all", stage]:
        for name in names:
            (OUTPUT / name).unlink(missing_ok=True)
signal.signal(signal.SIGTERM, lambda *_: sys.exit(143))
signal.signal(signal.SIGINT, lambda *_: sys.exit(130))
try:
    snapshot()
    versions = {}
    for name, argv in [("go", ["go", "version"]), ("node", ["node", "--version"]), ("codeql", [tool("codeql"), "version", "--format=json"]), ("k6", [tool("k6"), "version"]), ("collector", [tool("otelcol-contrib"), "--version"]), ("grafana", [tool("grafana"), "--version"])]:
        versions[name] = subprocess.check_output(argv, env=environment, text=True, stderr=subprocess.DEVNULL).strip()
    cpu = next((line.split(":", 1)[1].strip() for line in Path("/proc/cpuinfo").read_text().splitlines() if line.startswith("model name")), platform.processor())
    save(OUTPUT / "environment.json", {"recorded_at_utc": now(), "os": platform.platform(), "architecture": platform.machine(), "cpu": cpu, "logical_cpus": os.cpu_count(), "ram_bytes": os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_PHYS_PAGES"), "versions": versions, "measurement_isolation": "CodeQL and batch finish before k6; existing development services are not load targets"})
    for stage, action in [("codeql", codeql), ("batch", batch), ("telemetry", telemetry)]:
        if args.stage in ["all", stage]: action()
    report["status"] = "completed"
except BaseException as error:
    report["status"] = "failed"
    report["error"] = str(error)
    raise
finally:
    cleanup()
    report["finished_at_utc"] = now()
    save(OUTPUT / "run.json", report)
    print(f"[{now()}] proof status: {report['status']}; see output/run.json", flush=True)
