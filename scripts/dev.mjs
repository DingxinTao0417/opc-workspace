import { spawn, spawnSync } from "node:child_process";
import { mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = dirname(scriptsDir);
const webOnly = process.argv.includes("--web-only");
const goCommand = process.platform === "win32" ? "go.exe" : "go";
const pnpmCli = process.env.npm_execpath;
const devDataDir = join(repoRoot, ".local", "dev-data");
const databasePath = join(devDataDir, "opc-workspace.db");
const artifactDir = join(devDataDir, "artifacts");
const invoiceDir = join(devDataDir, "invoices");
const backupDir = join(devDataDir, "backups");
const logDir = join(devDataDir, "logs");
const apiBaseUrl = "http://127.0.0.1:9876";
const webBaseUrl = "http://127.0.0.1:1420";
const sessionToken = process.env.OPC_SESSION_TOKEN ?? "opc-workspace-local-dev";
const children = new Set();
let shuttingDown = false;

mkdirSync(devDataDir, { recursive: true });
mkdirSync(artifactDir, { recursive: true });
mkdirSync(invoiceDir, { recursive: true });
mkdirSync(backupDir, { recursive: true });
mkdirSync(logDir, { recursive: true });

function assertCommand(command, displayName, versionArgs = ["--version"]) {
  const probe = spawnSync(command, versionArgs, { stdio: "ignore" });
  if (probe.error || probe.status !== 0) {
    throw new Error(
      `${displayName} is required for local development but was not found on PATH.`,
    );
  }
}

function prefix(stream, name) {
  let pending = "";
  stream.setEncoding("utf8");
  stream.on("data", (chunk) => {
    pending += chunk;
    const lines = pending.split(/\r?\n/);
    pending = lines.pop() ?? "";
    for (const line of lines) {
      if (line) process.stdout.write(`[${name}] ${line}\n`);
    }
  });
  stream.on("end", () => {
    if (pending) process.stdout.write(`[${name}] ${pending}\n`);
  });
}

function start(name, command, args, options = {}) {
  const child = spawn(command, args, {
    cwd: options.cwd ?? repoRoot,
    env: { ...process.env, ...options.env },
    stdio: ["inherit", "pipe", "pipe"],
    windowsHide: true,
  });
  children.add(child);
  prefix(child.stdout, name);
  prefix(child.stderr, name);
  child.once("exit", (code, signal) => {
    children.delete(child);
    if (!shuttingDown && options.required !== false) {
      console.error(`[${name}] exited (${signal ?? code ?? "unknown"})`);
      void shutdown(code || 1);
    }
  });
  return child;
}

async function waitFor(label, check, timeoutMs = 45_000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      if (await check()) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(
    `${label} did not become ready: ${lastError?.message ?? "timeout"}`,
  );
}

function stopProcessTree(child) {
  if (!child.pid || child.exitCode !== null) return;
  if (process.platform === "win32") {
    spawnSync("taskkill", ["/PID", String(child.pid), "/T"], {
      stdio: "ignore",
      windowsHide: true,
    });
  } else {
    child.kill("SIGTERM");
  }
}

async function shutdown(exitCode = 0) {
  if (shuttingDown) return;
  shuttingDown = true;
  for (const child of [...children].reverse()) stopProcessTree(child);
  await new Promise((resolve) => setTimeout(resolve, 250));
  process.exit(exitCode);
}

process.once("SIGINT", () => void shutdown(0));
process.once("SIGTERM", () => void shutdown(0));

async function main() {
  assertCommand(goCommand, "Go 1.22+", ["version"]);
  if (!pnpmCli) {
    throw new Error(
      "Run this script through pnpm (`pnpm dev` or `pnpm dev:web`).",
    );
  }

  const sharedEnv = {
    OPC_SESSION_TOKEN: sessionToken,
    OPC_ALLOWED_ORIGINS: [
      webBaseUrl,
      "http://localhost:1420",
      "http://tauri.localhost",
      "https://tauri.localhost",
      "tauri://localhost",
    ].join(","),
  };

  start(
    "sidecar",
    goCommand,
    [
      "-C",
      join(repoRoot, "services", "sidecar"),
      "run",
      "./cmd/server",
      "--db",
      databasePath,
      "--artifacts",
      artifactDir,
      "--invoices",
      invoiceDir,
      "--backups",
      backupDir,
      "--logs",
      logDir,
      "--port",
      "9876",
      "--dev",
    ],
    { env: sharedEnv },
  );

  await waitFor("Go sidecar", async () => {
    const response = await fetch(`${apiBaseUrl}/health`, {
      headers: {
        Authorization: `Bearer ${sessionToken}`,
        Origin: webBaseUrl,
      },
    });
    return response.ok;
  });

  start("web", process.execPath, [pnpmCli, "--filter", "@opc/web", "dev"], {
    env: {
      ...sharedEnv,
      OPC_DEV_SIDECAR_URL: apiBaseUrl,
      VITE_OPC_SESSION_TOKEN: sessionToken,
    },
  });

  await waitFor("Vite", async () => (await fetch(webBaseUrl)).ok);
  console.log(`[dev] web ready at ${webBaseUrl}`);

  if (webOnly) return;

  const desktop = start(
    "desktop",
    process.execPath,
    [pnpmCli, "--filter", "@opc/desktop", "tauri:dev"],
    {
      required: false,
      env: {
        ...sharedEnv,
        OPC_SIDECAR_URL: apiBaseUrl,
        VITE_OPC_SESSION_TOKEN: sessionToken,
      },
    },
  );

  const [code, signal] = await new Promise((resolve) =>
    desktop.once("exit", (exitCode, exitSignal) =>
      resolve([exitCode, exitSignal]),
    ),
  );
  await shutdown(code ?? (signal ? 1 : 0));
}

main().catch((error) => {
  console.error(`[dev] ${error.message}`);
  void shutdown(1);
});
