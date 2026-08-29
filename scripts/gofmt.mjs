import { execFileSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { dirname, join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = dirname(scriptsDir);
const goRoot = join(repoRoot, "services", "sidecar");
const write = process.argv.slice(2).includes("--write");
const batchSize = 64;

function collectGoFiles(directory) {
  return readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const entryPath = join(directory, entry.name);
      if (entry.isDirectory()) return collectGoFiles(entryPath);
      return entry.isFile() && entry.name.endsWith(".go") ? [entryPath] : [];
    })
    .sort();
}

const files = collectGoFiles(goRoot);
const unformatted = [];

for (let index = 0; index < files.length; index += batchSize) {
  const batch = files.slice(index, index + batchSize);
  const output = execFileSync("gofmt", [write ? "-w" : "-l", ...batch], {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
  });
  if (!write) {
    unformatted.push(...output.split(/\r?\n/).filter(Boolean));
  }
}

if (write) {
  console.log(`[gofmt] formatted ${files.length} Go files`);
} else if (unformatted.length > 0) {
  console.error("[gofmt] these Go files need formatting:");
  for (const file of unformatted) {
    console.error(`- ${relative(repoRoot, file)}`);
  }
  process.exitCode = 1;
} else {
  console.log(`[gofmt] ${files.length} Go files are formatted`);
}
