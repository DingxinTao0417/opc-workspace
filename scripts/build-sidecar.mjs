import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = dirname(scriptsDir);
const sidecarDir = join(repoRoot, "services", "sidecar");
const binaryDir = join(repoRoot, "apps", "desktop", "src-tauri", "binaries");
const packageJson = JSON.parse(
  readFileSync(join(repoRoot, "package.json"), "utf8"),
);

function commandOutput(command, args) {
  return execFileSync(command, args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
  }).trim();
}

function buildCommit() {
  const explicit = process.env.OPC_BUILD_COMMIT?.trim();
  if (explicit && /^[0-9A-Za-z._+-]{1,128}$/.test(explicit)) return explicit;
  try {
    const revision = commandOutput("git", ["rev-parse", "--short=12", "HEAD"]);
    const dirty = commandOutput("git", ["status", "--porcelain"]);
    return `${revision}${dirty ? "-dirty" : ""}`;
  } catch {
    return "unknown";
  }
}

const targetTriple = commandOutput("rustc", ["--print", "host-tuple"]);
const extension = process.platform === "win32" ? ".exe" : "";
const output = join(binaryDir, `opc-sidecar-${targetTriple}${extension}`);
const commit = buildCommit();

mkdirSync(binaryDir, { recursive: true });

console.log(`[sidecar] building ${targetTriple}`);
console.log(`[sidecar] commit ${commit}`);
execFileSync(
  "go",
  [
    "build",
    "-trimpath",
    "-ldflags",
    `-s -w -X main.appVersion=${packageJson.version} -X main.commit=${commit}`,
    "-o",
    output,
    "./cmd/server",
  ],
  {
    cwd: sidecarDir,
    env: { ...process.env, CGO_ENABLED: "0" },
    stdio: "inherit",
  },
);

console.log(`[sidecar] wrote ${output}`);
