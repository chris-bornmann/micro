import { defineConfig } from "vitest/config";

// JSX is compiled for Preact from tsconfig.json's jsxImportSource; no
// framework plugin is needed.
export default defineConfig({
  server: { port: 5173, strictPort: true },
  build: { target: "es2022", sourcemap: true },
  test: { environment: "node", include: ["src/**/*.test.ts"] },
});
