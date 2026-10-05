import assert from "node:assert/strict";
import { test } from "node:test";
import { temporaryDirectoryProblem } from "./check-temp.mjs";

test("a temporary directory that is its own real path is accepted", () => {
  assert.equal(
    temporaryDirectoryProblem(
      "C:\\Users\\dev\\AppData\\Local\\Temp",
      "C:\\Users\\dev\\AppData\\Local\\Temp",
      "win32",
    ),
    null,
  );
  // Drive-letter case and a trailing separator are not differences.
  assert.equal(
    temporaryDirectoryProblem("c:\\Temp\\", "C:\\Temp", "win32"),
    null,
  );
});

test("a junction or an 8.3 short name is reported with both paths", () => {
  for (const [configured, real] of [
    ["D:\\Tools\\temp", "D:\\Caches\\Tools\\temp"],
    [
      "C:\\Users\\RUNNER~1\\AppData\\Local\\Temp",
      "C:\\Users\\runneradmin\\AppData\\Local\\Temp",
    ],
  ]) {
    const problem = temporaryDirectoryProblem(configured, real, "win32");
    assert.ok(problem?.includes(configured), configured);
    assert.ok(problem?.includes(real), real);
    assert.match(problem, /TEMP and TMP/);
  }
});

test("other platforms are not checked", () => {
  assert.equal(
    temporaryDirectoryProblem(
      "/var/folders/x/T",
      "/private/var/folders/x/T",
      "darwin",
    ),
    null,
  );
});
