import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

// A suppressed finding has to say why: `// eslint-disable-next-line rule -- reason`.
const directiveReason = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      missing: "Give the reason after ' -- ' when disabling a rule.",
    },
  },
  create(context) {
    return {
      Program() {
        for (const comment of context.sourceCode.getAllComments()) {
          const text = comment.value.trim();
          if (/^eslint-disable/.test(text) && !/\s--\s+\S/.test(text)) {
            context.report({ loc: comment.loc, messageId: "missing" });
          }
        }
      },
    };
  },
};

export default defineConfig([
  globalIgnores(["dist", "npm-cache"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, tseslint.configs.recommended],
    languageOptions: { ecmaVersion: 2022, globals: globals.browser },
    plugins: {
      "react-hooks": reactHooks,
      local: { rules: { "directive-reason": directiveReason } },
    },
    rules: {
      // The two rules that guard hook correctness. The plugin's React Compiler
      // rule family is not enabled: this project does not use the compiler.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
      "local/directive-reason": "error",
      // A leading underscore marks a binding that is unused on purpose: a
      // positional parameter kept for the signature, or a property pulled out
      // of an object so that the rest omits it.
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
          ignoreRestSiblings: true,
        },
      ],
    },
  },
  {
    // `Untyped` is the one sanctioned `any`; it exists for test fixtures only.
    files: ["**/*.{ts,tsx}"],
    ignores: ["**/*.test.{ts,tsx}", "src/test/**"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["**/test/untyped"],
              message: "Untyped is for test fixtures only.",
            },
          ],
        },
      ],
    },
  },
  { files: ["*.config.{ts,js}"], languageOptions: { globals: globals.node } },
]);
