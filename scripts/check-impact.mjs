import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// Impact report: turns a diff into the smallest set of repository checks that
// can be affected by it. Lane selection stays conservative — anything it cannot
// classify, or any file that fans out to several surfaces, selects every lane.

const scriptsDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = dirname(scriptsDir);

const LANES = [
  {
    id: "format:check",
    command: "pnpm format:check",
    matches: () => true,
  },
  {
    id: "check:docs",
    command: "pnpm check:docs",
    matches: (file) => file.endsWith(".md") || file.startsWith("docs/"),
  },
  {
    id: "check:web",
    command: "pnpm check:web",
    matches: (file) => file.startsWith("apps/web/"),
  },
  {
    id: "check:go",
    command: "pnpm check:go",
    matches: (file) => file.startsWith("services/sidecar/"),
  },
  {
    id: "check:rust",
    command: "pnpm check:rust",
    matches: (file) =>
      file.startsWith("apps/desktop/src-tauri/") ||
      file.startsWith("apps/file-security-helper/") ||
      file === "apps/desktop/package.json",
  },
];

// Files whose change can reach every surface: workspace wiring, shared scripts,
// CI definitions and toolchain configuration.
const HUB_PATTERNS = [
  /^package\.json$/,
  /^pnpm-workspace\.yaml$/,
  /^pnpm-lock\.yaml$/,
  /^tsconfig[^/]*\.json$/,
  /^scripts\//,
  /^\.github\//,
  /^apps\/web\/(vite|vitest|tsconfig[^/]*)\.(config\.)?(ts|json)$/,
  /^services\/sidecar\/go\.(mod|sum)$/,
  /^apps\/desktop\/src-tauri\/(Cargo\.(toml|lock)|tauri\.conf\.json|build\.rs)$/,
  /^apps\/file-security-helper\/Cargo\.(toml|lock)$/,
];

function git(args) {
  return execFileSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "ignore"],
  })
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

function resolveBase(explicit) {
  if (explicit) return explicit;
  // Local iteration means "everything this branch has not pushed yet"; a PR
  // review means the whole branch against main. Prefer the former.
  try {
    const upstream = git([
      "rev-parse",
      "--abbrev-ref",
      "--symbolic-full-name",
      "@{upstream}",
    ])[0];
    if (upstream) return upstream;
  } catch {
    /* no upstream configured */
  }
  try {
    return git(["merge-base", "HEAD", "origin/main"])[0] ?? "HEAD";
  } catch {
    return "HEAD";
  }
}

function changedFiles(base) {
  const files = new Set();
  const collect = (args) => {
    try {
      for (const file of git(args)) files.add(file.replaceAll("\\", "/"));
    } catch {
      // A missing ref (fresh clone without origin/main, shallow CI checkout)
      // must not silently narrow the report.
      files.add("<unresolved>");
    }
  };
  collect(["diff", "--name-only", `${base}...HEAD`]);
  collect(["diff", "--name-only", "HEAD"]);
  collect(["ls-files", "--others", "--exclude-standard"]);
  return [...files].filter((file) => file !== "<unresolved>").sort();
}

function selectLanes(files) {
  const selected = new Map();
  const reasons = new Map();
  const unknown = [];
  const hubs = files.filter((file) =>
    HUB_PATTERNS.some((pattern) => pattern.test(file)),
  );
  const runEverything = hubs.length > 0 || files.length === 0;

  for (const lane of LANES) {
    if (runEverything || files.some((file) => lane.matches(file))) {
      selected.set(lane.id, lane.command);
    }
  }
  for (const file of files) {
    const owners = LANES.filter((lane) => lane.matches(file)).map(
      (lane) => lane.id,
    );
    if (owners.length === 0) unknown.push(file);
    for (const owner of owners) {
      const list = reasons.get(owner) ?? [];
      if (list.length < 4) list.push(file);
      reasons.set(owner, list);
    }
  }
  if (hubs.length > 0) {
    for (const [id] of selected) {
      reasons.set(id, [`hub: ${hubs.slice(0, 4).join(", ")}`]);
    }
  }
  return { selected, reasons, unknown, hubs, runEverything };
}

// Test selection is the reverse-dependency half of the report: which existing
// test files can reach the changed sources. It only ever *suggests* a narrower
// local command; the lane report above stays conservative.
const IMPORT_PATTERN =
  /(?:^|[^\w.])(?:import|export)[^;]*?from\s*["']([^"']+)["']|import\(\s*["']([^"']+)["']/g;

function collectSourceFiles(directory, files = []) {
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const entryPath = join(directory, entry.name);
    if (entry.isDirectory()) {
      collectSourceFiles(entryPath, files);
      continue;
    }
    if (/\.(ts|tsx)$/.test(entry.name)) files.push(entryPath);
  }
  return files;
}

function resolveSpecifier(fromFile, specifier) {
  if (!specifier.startsWith(".")) return null;
  const base = resolve(dirname(fromFile), specifier);
  for (const candidate of [
    base,
    `${base}.ts`,
    `${base}.tsx`,
    join(base, "index.ts"),
    join(base, "index.tsx"),
  ]) {
    if (/\.(ts|tsx)$/.test(candidate)) {
      try {
        readFileSync(candidate);
        return candidate;
      } catch {
        /* try the next candidate */
      }
    }
  }
  return null;
}

function affectedTests(files) {
  const webRoot = join(repoRoot, "apps", "web", "src");
  let sources;
  try {
    sources = collectSourceFiles(webRoot);
  } catch {
    return null;
  }
  const importers = new Map();
  for (const file of sources) {
    let contents;
    try {
      contents = readFileSync(file, "utf8");
    } catch {
      continue;
    }
    for (const match of contents.matchAll(IMPORT_PATTERN)) {
      const target = resolveSpecifier(file, match[1] ?? match[2] ?? "");
      if (!target) continue;
      const list = importers.get(target) ?? [];
      list.push(file);
      importers.set(target, list);
    }
  }
  const roots = files
    .filter((file) => file.startsWith("apps/web/src/"))
    .map((file) => join(repoRoot, file));
  if (roots.length === 0) return { tests: [], truncated: false };
  const seen = new Set(roots);
  const queue = [...roots];
  const tests = new Set();
  while (queue.length > 0) {
    const current = queue.shift();
    for (const importer of importers.get(current) ?? []) {
      if (seen.has(importer)) continue;
      seen.add(importer);
      if (/\.test\.tsx?$/.test(importer)) tests.add(importer);
      queue.push(importer);
    }
  }
  return {
    tests: [...tests]
      .map((file) => relative(webRoot, file).replaceAll("\\", "/"))
      .sort(),
    truncated: false,
  };
}

function main() {
  const args = process.argv.slice(2);
  const baseIndex = args.indexOf("--base");
  const base = resolveBase(baseIndex >= 0 ? args[baseIndex + 1] : undefined);
  const files = changedFiles(base);
  const { selected, reasons, unknown, hubs, runEverything } =
    selectLanes(files);
  const tests = affectedTests(files);

  if (args.includes("--json")) {
    process.stdout.write(
      `${JSON.stringify(
        {
          base,
          files,
          lanes: [...selected].map(([id, command]) => ({
            id,
            command,
            reason: reasons.get(id) ?? [],
          })),
          unknown,
          hubs,
          affectedTests: tests?.tests ?? null,
        },
        null,
        2,
      )}\n`,
    );
    return;
  }

  const lines = ["## Impact report", `base: ${base}`];
  if (files.length === 0) {
    lines.push("no changed files detected; every lane is selected");
  } else {
    lines.push(`changed files: ${files.length}`);
  }
  if (hubs.length > 0) {
    lines.push(`hub files (all lanes): ${hubs.join(", ")}`);
  }
  if (unknown.length > 0) {
    lines.push(
      `unclassified files (conservative, already covered): ${unknown.join(", ")}`,
    );
  }
  lines.push("", "selected lanes:");
  for (const [id, command] of selected) {
    const reason = reasons.get(id) ?? [];
    lines.push(`- ${id}${reason.length ? `  (${reason.join("; ")})` : ""}`);
  }
  lines.push("", "commands:");
  for (const [, command] of selected) lines.push(command);
  if (tests && tests.tests.length > 0) {
    const shown = tests.tests.slice(0, 12);
    const rest = tests.tests.length - shown.length;
    lines.push(
      "",
      `possibly affected web tests (${tests.tests.length}) via reverse imports,`,
      "for narrow local iteration:",
      `pnpm --filter @opc/web test -- ${shown.join(" ")}`,
    );
    if (rest > 0) {
      lines.push(
        `(+${rest} more; a change to a widely imported module reaches most of the suite)`,
      );
    }
  }
  if (runEverything) {
    lines.push("", "note: no file-level narrowing applied for this diff");
  }
  process.stdout.write(`${lines.join("\n")}\n`);
}

main();
