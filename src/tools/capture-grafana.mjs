// Capture the actual provisioned Grafana dashboard without publishing login data.
import { spawn } from "node:child_process";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

const [passwordFile, destination] = process.argv.slice(2);
if (!passwordFile || !destination) throw new Error("Usage: node capture-grafana.mjs PASSWORD_FILE PNG_OUTPUT");
const base = "http://127.0.0.1:3030";
const password = (await readFile(passwordFile, "utf8")).trim();
const login = await fetch(`${base}/login`, {
  method: "POST", headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ user: "admin", password }),
});
if (!login.ok) throw new Error(`Grafana login returned ${login.status}`);
const cookies = login.headers.getSetCookie().map(value => {
  const pair = value.split(";", 1)[0];
  return { name: pair.slice(0, pair.indexOf("=")), value: pair.slice(pair.indexOf("=") + 1), url: base };
});
const profile = await mkdtemp(join(tmpdir(), "schakker-grafana-"));
const browser = spawn(process.env.SCHAKKER_QA_BROWSER || "/usr/bin/google-chrome", [
  "--headless=new", "--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu",
  "--remote-debugging-port=0", `--user-data-dir=${profile}`, "about:blank",
], { stdio: "ignore" });
let socket;
const pause = milliseconds => new Promise(resolve => setTimeout(resolve, milliseconds));
try {
  let port;
  for (let attempt = 0; attempt < 100; attempt++) {
    try { port = (await readFile(join(profile, "DevToolsActivePort"), "utf8")).split("\n")[0]; break; } catch { await pause(100); }
  }
  if (!port) throw new Error("Chrome did not become ready");
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
  socket = new WebSocket(targets.find(target => target.type === "page").webSocketDebuggerUrl);
  await new Promise((resolve, reject) => { socket.addEventListener("open", resolve, { once: true }); socket.addEventListener("error", reject, { once: true }); });
  let sequence = 0;
  const pending = new Map();
  socket.addEventListener("message", event => {
    const message = JSON.parse(event.data);
    const call = pending.get(message.id);
    if (!call) return;
    pending.delete(message.id);
    if (message.error) call.reject(new Error(message.error.message)); else call.resolve(message.result);
  });
  const cdp = (method, params = {}) => new Promise((resolve, reject) => {
    const id = ++sequence;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params }));
  });
  await cdp("Network.enable");
  await cdp("Network.setCookies", { cookies });
  await cdp("Emulation.setDeviceMetricsOverride", { width: 1440, height: 1800, deviceScaleFactor: 1, mobile: false });
  await cdp("Page.navigate", { url: `${base}/d/schakker-migrations?orgId=1&from=now-10m&to=now&refresh=5s` });
  let rendered = false;
  for (let attempt = 0; attempt < 90; attempt++) {
    const { result } = await cdp("Runtime.evaluate", { expression: "document.body?.innerText || ''", returnByValue: true });
    if (result.value.includes("Schakker") && result.value.includes("connection") && !result.value.includes("Sign in to Grafana")) { rendered = true; break; }
    await pause(1000);
  }
  if (!rendered) throw new Error("Provisioned Grafana dashboard did not render");
  await pause(10000);
  const screenshot = await cdp("Page.captureScreenshot", { format: "png", captureBeyondViewport: true });
  await writeFile(destination, Buffer.from(screenshot.data, "base64"));
  console.log("Captured authenticated Grafana dashboard");
} finally {
  socket?.close();
  browser.kill("SIGTERM");
  await Promise.race([new Promise(resolve => browser.once("exit", resolve)), pause(5000)]);
  if (browser.exitCode === null) browser.kill("SIGKILL");
  await rm(profile, { recursive: true, force: true });
}
