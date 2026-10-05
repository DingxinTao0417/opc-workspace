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
const heading = /^#{1,6}\s+(.*?)\s*#*\s*$/;
const fence = /^\s*(`{3,}|~{3,})/;

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

function splitTarget(rawTarget) {
  const target = rawTarget.startsWith("<") ? rawTarget.slice(1, -1) : rawTarget;
  const hash = target.indexOf("#");
  return hash === -1
    ? { target, path: target, fragment: null }
    : {
        target,
        path: target.slice(0, hash),
        fragment: target.slice(hash + 1),
      };
}

function decode(value, what) {
  try {
    return decodeURIComponent(value);
  } catch {
    throw new Error(`link ${what} is not valid URI encoding`);
  }
}

function resolveLocalTarget(sourceFile, rawTarget) {
  const { target, path, fragment } = splitTarget(rawTarget);
  if (windowsAbsolutePath.test(target)) {
    throw new Error("absolute machine path is not allowed");
  }
  if (externalScheme.test(target)) return null;

  const decodedPath = decode(path, "path");
  const file = !decodedPath
    ? sourceFile
    : isAbsolute(decodedPath)
      ? resolve(repoRoot, `.${decodedPath}`)
      : resolve(dirname(sourceFile), decodedPath);
  return {
    file,
    fragment: fragment ? decode(fragment, "fragment") : null,
  };
}

// The text a heading renders to: link and image labels without their targets,
// inline HTML removed.
function headingText(raw) {
  return raw.replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1").replace(/<[^>]+>/g, "");
}

// Mirrors the anchors GitHub generates for headings: lower-case, keep only
// letters, marks, digits, underscores, hyphens and spaces, then turn spaces
// into hyphens. A repeated heading gets a -1, -2, ... suffix.
function slugify(text) {
  return text
    .toLowerCase()
    .replace(/[^\p{L}\p{M}\p{N}\p{Pc}\- ]/gu, "")
    .replace(/ /g, "-");
}

function collectAnchors(source) {
  const anchors = new Set();
  const seen = new Map();
  let openFence = null;
  for (const line of source.split(/\r?\n/)) {
    const marker = line.match(fence)?.[1][0];
    if (marker) {
      if (openFence === null) openFence = marker;
      else if (openFence === marker) openFence = null;
      continue;
    }
    if (openFence !== null) continue;
    const match = line.match(heading);
    if (!match) continue;
    const base = slugify(headingText(match[1]));
    const count = seen.get(base) ?? 0;
    seen.set(base, count + 1);
    anchors.add(count === 0 ? base : `${base}-${count}`);
  }
  for (const match of source.matchAll(/<a\s+(?:name|id)="([^"]+)"/g)) {
    anchors.add(match[1]);
  }
  return anchors;
}

const files = [
  join(repoRoot, "README.md"),
  join(repoRoot, "README.en.md"),
  join(repoRoot, "AGENTS.md"),
  join(repoRoot, "services", "sidecar", "README.md"),
  ...collectMarkdownFiles(docsRoot),
];
const errors = [];
const anchorCache = new Map();

function anchorsOf(file) {
  if (!anchorCache.has(file)) {
    anchorCache.set(file, collectAnchors(readFileSync(file, "utf8")));
  }
  return anchorCache.get(file);
}

let anchorCount = 0;
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
      if (!target) continue;
      if (!existsSync(target.file)) {
        errors.push(
          `${displayPath(file)}: missing link target ${rawTarget} (${displayPath(target.file)})`,
        );
        continue;
      }
      if (target.fragment === null || !target.file.endsWith(".md")) continue;
      anchorCount += 1;
      if (!anchorsOf(target.file).has(target.fragment)) {
        errors.push(
          `${displayPath(file)}: missing heading anchor ${rawTarget} (no heading in ${displayPath(target.file)} produces #${target.fragment})`,
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
  console.log(
    `[docs] checked ${files.length} Markdown files, local links and ${anchorCount} heading anchors`,
  );
}
