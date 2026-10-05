import assert from "node:assert/strict";
import { test } from "node:test";
import { selectLanes } from "./check-impact.mjs";

const lanes = (files) => [...selectLanes(files).selected.keys()];
const everyLane = [
  "format:check",
  "check:docs",
  "check:scripts",
  "check:web",
  "check:go",
  "check:rust",
];

test("CI definitions and repository-wide configuration select every lane", () => {
  for (const file of [
    ".github/workflows/check.yml",
    ".gitattributes",
    ".editorconfig",
    "package.json",
    "pnpm-lock.yaml",
    "scripts/check-docs.mjs",
  ]) {
    const result = selectLanes([file]);
    assert.deepEqual([...result.selected.keys()], everyLane, file);
    assert.deepEqual(result.hubs, [file], file);
  }
});

test("a change inside one surface selects only that surface", () => {
  assert.deepEqual(lanes(["apps/web/src/App.tsx"]), [
    "format:check",
    "check:web",
  ]);
  // The lint configuration reaches nothing outside the web gate.
  assert.deepEqual(lanes(["apps/web/eslint.config.js"]), [
    "format:check",
    "check:web",
  ]);
  assert.deepEqual(lanes(["services/sidecar/internal/api/router.go"]), [
    "format:check",
    "check:go",
  ]);
  assert.deepEqual(lanes(["apps/desktop/src-tauri/src/main.rs"]), [
    "format:check",
    "check:rust",
  ]);
  assert.deepEqual(lanes(["docs/changelog.md"]), [
    "format:check",
    "check:docs",
  ]);
});

test("nothing detected means nothing can be ruled out", () => {
  const result = selectLanes([]);
  assert.deepEqual([...result.selected.keys()], everyLane);
  assert.equal(result.runEverything, true);
});
