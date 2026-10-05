import { realpathSync } from "node:fs";
import { tmpdir } from "node:os";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

// Preflight for the Go and Rust gates on Windows.
//
// The controlled file stores refuse a directory whose path is not its own real
// path, so tests that create their data under the temporary directory fail by
// the hundred when TEMP is reached through a junction, a symbolic link or an
// 8.3 short name (the default on hosted Windows CI runners). This turns that
// into one message that says what to change.

const strip = (path) => path.replace(/[\\/]+$/, "");

// Returns a description of the problem, or null when the directory is usable.
export function temporaryDirectoryProblem(configured, real, platform) {
  if (platform !== "win32") return null;
  if (strip(configured).toLowerCase() === strip(real).toLowerCase())
    return null;
  return [
    `The temporary directory is ${configured}`,
    `but its real path is ${real}.`,
    "It is reached through a junction, a symbolic link or an 8.3 short name,",
    "which the safe-directory checks in the Go and Rust tests reject.",
    "Point TEMP and TMP at a real directory and run the gate again.",
  ].join("\n");
}

if (
  process.argv[1] &&
  resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  const configured = tmpdir();
  let real;
  try {
    real = realpathSync.native(configured);
  } catch (error) {
    console.error(
      `[temp] cannot resolve the temporary directory ${configured}: ${error.message}`,
    );
    process.exit(1);
  }
  const problem = temporaryDirectoryProblem(configured, real, process.platform);
  if (problem) {
    console.error(`[temp] ${problem.split("\n").join("\n[temp] ")}`);
    process.exit(1);
  }
}
