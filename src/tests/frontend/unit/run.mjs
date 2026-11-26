// Compile the actual TS/TSX modules, then run Node's built-in test runner.
import { build } from "esbuild";
import { spawn } from "node:child_process";
import { mkdir, mkdtemp, readdir, rm } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
const directory = dirname(fileURLToPath(import.meta.url));
const root = resolve(directory, "../../../..");
const entries = (await readdir(directory)).filter(name => /\.test\.tsx?$/.test(name)).sort().map(name => join(directory, name));
await mkdir(join(root, ".schakker"), { recursive: true });
const output = await mkdtemp(join(root, ".schakker/frontend-unit-"));
try {
  await build({ entryPoints: entries, outdir: output, outExtension: { ".js": ".mjs" }, bundle: true, format: "esm", platform: "node", target: "node22", jsx: "automatic", sourcemap: "inline", external: ["react", "react-dom", "react-dom/*", "react-router-dom"] });
  const tests = entries.map(entry => join(output, entry.split(/[\\/]/).at(-1).replace(/\.tsx?$/, ".mjs")));
  const child = spawn(process.execPath, ["--enable-source-maps", "--test", ...tests], { stdio: "inherit", cwd: root });
  const code = await new Promise((done, reject) => { child.once("error", reject); child.once("exit", done); });
  process.exitCode = code ?? 1;
} finally { await rm(output, { recursive: true, force: true }); }
