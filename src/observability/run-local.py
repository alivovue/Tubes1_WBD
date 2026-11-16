#!/usr/bin/env python3
"""Run the pinned observability configs with locally installed portable binaries."""
import argparse
import os
from pathlib import Path
import secrets
import shutil
import signal
import subprocess
import time

parser = argparse.ArgumentParser()
parser.add_argument('--tools-directory', type=Path, help='Search extracted official release archives here')
parser.add_argument('--data-directory', type=Path, required=True, help='Persistent local data and service logs')
args = parser.parse_args()
root = Path(__file__).resolve().parent
data = args.data_directory.resolve()
data.mkdir(parents=True, exist_ok=True)

def binary(name):
    if args.tools_directory:
        for path in args.tools_directory.resolve().rglob(name):
            if path.is_file() and os.access(path, os.X_OK):
                return path
    found = shutil.which(name)
    if found:
        return Path(found)
    raise SystemExit(f'Install the official {name} binary or provide --tools-directory')

collector = binary('otelcol-contrib')
loki = binary('loki-linux-amd64') if args.tools_directory else binary('loki')
tempo = binary('tempo')
prometheus = binary('prometheus')
grafana = binary('grafana')
# The standalone Grafana distribution needs its conf/public directories.
grafana_home = grafana.parent.parent
provisioning = data / 'grafana-provisioning'
shutil.copytree(root / 'grafana/provisioning', provisioning, dirs_exist_ok=True)
providers = provisioning / 'dashboards/schakker.yaml'
providers.write_text(providers.read_text().replace('/var/lib/grafana/dashboards', str(root / 'grafana/dashboards')))
credential = data / 'grafana_password'
if not credential.exists():
    credential.write_text(secrets.token_urlsafe(32) + '\n')
credential.chmod(0o600)
environment = dict(os.environ, LOKI_DATA_DIR=str(data / 'loki'), TEMPO_DATA_DIR=str(data / 'tempo'),
    GF_PATHS_DATA=str(data / 'grafana'), GF_PATHS_LOGS=str(data / 'grafana-logs'),
    GF_PATHS_PROVISIONING=str(provisioning), GF_SERVER_HTTP_ADDR='127.0.0.1', GF_SERVER_HTTP_PORT='3030',
    GF_AUTH_ANONYMOUS_ENABLED='false', GF_USERS_ALLOW_SIGN_UP='false',
    GF_SECURITY_ADMIN_PASSWORD=credential.read_text().strip(),
    GF_ANALYTICS_REPORTING_ENABLED='false', GF_ANALYTICS_CHECK_FOR_UPDATES='false')
processes = []
logs = []
stopping = False

def stop(_signum=None, _frame=None):
    global stopping
    stopping = True

signal.signal(signal.SIGTERM, stop)
signal.signal(signal.SIGINT, stop)
try:
    commands = [
        ('loki', [str(loki), '-config.file=' + str(root / 'loki.yaml'), '-config.expand-env=true']),
        ('tempo', [str(tempo), '-config.file=' + str(root / 'tempo.yaml'), '-config.expand-env=true']),
        ('prometheus', [str(prometheus), '--config.file=' + str(root / 'prometheus.yml'), '--storage.tsdb.path=' + str(data / 'prometheus'), '--storage.tsdb.retention.time=24h', '--web.listen-address=127.0.0.1:9090', '--enable-feature=exemplar-storage']),
        ('collector', [str(collector), '--config=' + str(root / 'collector.yaml')]),
        ('grafana', [str(grafana), 'server', '--homepath=' + str(grafana_home)]),
    ]
    for name, command in commands:
        log = (data / (name + '.log')).open('a')
        logs.append(log)
        processes.append((name, subprocess.Popen(command, env=environment, cwd=data, stdout=log, stderr=subprocess.STDOUT)))
    print('Observability started on loopback. Grafana: http://127.0.0.1:3030', flush=True)
    print('Grafana user: admin; credential file:', credential, flush=True)
    while not stopping:
        for name, process in processes:
            if process.poll() is not None:
                raise SystemExit(f'{name} exited ({process.returncode}); inspect {data / (name + ".log")}')
        time.sleep(0.25)
finally:
    for _, process in reversed(processes):
        if process.poll() is None:
            process.terminate()
    for _, process in reversed(processes):
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
    for log in logs:
        log.close()
