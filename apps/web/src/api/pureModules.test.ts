import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";
import { expect, it } from "vitest";

// vite.config.ts tells Rollup that these modules have no import-time side
// effects, so a production build may skip evaluating one whose exports are not
// used. That is only safe while they contain nothing but declarations.
const srcRoot = dirname(dirname(fileURLToPath(import.meta.url)));
const pureDirectories = [
  "api/client",
  "api/hooks",
  "api/aiWorkspaceActions",
  "types/models",
];
const barrels = [
  "api/client.ts",
  "api/hooks.ts",
  "api/aiWorkspaceActions.ts",
  "types/models.ts",
];
// Building a value is fine; calling into another module or the platform at
// import time is not.
const allowedInitialisers = new Set([
  "new Set",
  "new Map",
  "new WeakMap",
  "Object.freeze",
]);

function parse(file: string) {
  return ts.createSourceFile(
    file,
    readFileSync(file, "utf8"),
    ts.ScriptTarget.Latest,
    true,
  );
}

function importTimeProblems(file: string) {
  const source = parse(file);
  const problems: string[] = [];
  for (const statement of source.statements) {
    if (
      ts.isImportDeclaration(statement) ||
      ts.isExportDeclaration(statement) ||
      ts.isFunctionDeclaration(statement) ||
      ts.isInterfaceDeclaration(statement) ||
      ts.isTypeAliasDeclaration(statement)
    )
      continue;
    if (
      !ts.isVariableStatement(statement) &&
      !ts.isClassDeclaration(statement)
    ) {
      problems.push(`${ts.SyntaxKind[statement.kind]} statement`);
      continue;
    }
    const visit = (node: ts.Node) => {
      if (ts.isFunctionLike(node)) return;
      if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
        const call = `${ts.isNewExpression(node) ? "new " : ""}${node.expression.getText(source)}`;
        if (!allowedInitialisers.has(call)) problems.push(`calls ${call}`);
      }
      ts.forEachChild(node, visit);
    };
    visit(statement);
  }
  return problems;
}

it("keeps the split API and model modules free of import-time side effects", () => {
  const offenders: string[] = [];
  for (const directory of pureDirectories) {
    for (const name of readdirSync(join(srcRoot, directory))) {
      for (const problem of importTimeProblems(join(srcRoot, directory, name)))
        offenders.push(`${directory}/${name}: ${problem}`);
    }
  }
  expect(offenders).toEqual([]);
});

it("keeps the barrels as pure re-export lists", () => {
  const offenders: string[] = [];
  for (const barrel of barrels) {
    for (const statement of parse(join(srcRoot, barrel)).statements) {
      if (!ts.isExportDeclaration(statement))
        offenders.push(`${barrel}: ${ts.SyntaxKind[statement.kind]}`);
    }
  }
  expect(offenders).toEqual([]);
});
