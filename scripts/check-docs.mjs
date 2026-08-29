import { existsSync, readFileSync, readdirSync } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = dirname(scriptsDir);
const docsRoot = join(repoRoot, "docs");
const markdownLink = /!?\[[^\]]*\]\((<[^>]+>|[^)\s]+)(?:\s+["'][^)]*["'])?\)/g;
const externalScheme = /^[a-z][a-z0-9+.-]*:/i;
const windowsAbsolutePath = /^[a-z]:[\\/]/i;
const workspaceMachinePath = /[a-z]:[\\/]workspace[\\/]/i;
const conflictMarker = /^(?:<{7}|={7}|>{7})(?:\s|$)/m;

function collectMarkdownFiles(directory) {
  return readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const entryPath = join(directory, entry.name);
      if (entry.isDirectory()) return collectMarkdownFiles(entryPath);
      return entry.isFile() && entry.name.endsWith(".md") ? [entryPath] : [];
    })
    .sort();
}

function displayPath(path) {
  return relative(repoRoot, path).replaceAll("\\", "/");
}

function resolveLocalTarget(sourceFile, rawTarget) {
  const target = rawTarget.startsWith("<") ? rawTarget.slice(1, -1) : rawTarget;
  if (windowsAbsolutePath.test(target)) {
    throw new Error("absolute machine path is not allowed");
  }
  if (target.startsWith("#") || externalScheme.test(target)) return null;

  const encodedPath = target.split("#", 1)[0];
  if (!encodedPath) return null;
  let decodedPath;
  try {
    decodedPath = decodeURIComponent(encodedPath);
  } catch {
    throw new Error("link path is not valid URI encoding");
  }
  return isAbsolute(decodedPath)
    ? resolve(repoRoot, `.${decodedPath}`)
    : resolve(dirname(sourceFile), decodedPath);
}

const files = [join(repoRoot, "README.md"), ...collectMarkdownFiles(docsRoot)];
const errors = [];

for (const file of files) {
  const source = readFileSync(file, "utf8");
  if (conflictMarker.test(source)) {
    errors.push(`${displayPath(file)}: contains a merge-conflict marker`);
  }
  if (workspaceMachinePath.test(source)) {
    errors.push(
      `${displayPath(file)}: contains a machine-specific workspace path`,
    );
  }

  for (const match of source.matchAll(markdownLink)) {
    const rawTarget = match[1];
    try {
      const target = resolveLocalTarget(file, rawTarget);
      if (target && !existsSync(target)) {
        errors.push(
          `${displayPath(file)}: missing link target ${rawTarget} (${displayPath(target)})`,
        );
      }
    } catch (error) {
      errors.push(`${displayPath(file)}: ${rawTarget}: ${error.message}`);
    }
  }
}

if (errors.length > 0) {
  console.error("[docs] documentation checks failed:");
  for (const error of errors) console.error(`- ${error}`);
  process.exitCode = 1;
} else {
  console.log(`[docs] checked ${files.length} Markdown files and local links`);
}
