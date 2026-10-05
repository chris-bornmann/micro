import js from "@eslint/js";
import tseslint from "typescript-eslint";
import globals from "globals";

export default tseslint.config(
  { ignores: ["dist", "node_modules"] },
  js.configs.recommended,
  ...tseslint.configs.strict,
  { languageOptions: { globals: globals.browser } },
  // The layers depend only downward: api, state and input never reach the
  // UI or a concrete renderer, and render never reaches state or the API.
  {
    files: ["src/api/**", "src/state/**", "src/input/**"],
    rules: {
      "no-restricted-imports": ["error", { patterns: ["**/ui/**", "**/render/rot*", "preact", "preact/*"] }],
    },
  },
  {
    files: ["src/render/**"],
    rules: {
      "no-restricted-imports": ["error", { patterns: ["**/state/**", "**/ui/**", "**/api/**"] }],
    },
  },
);
