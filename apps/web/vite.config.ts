import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

// The API client, hooks, AI action contract and model modules only declare
// functions and constants (src/api/pureModules.test.ts keeps that true).
// Telling Rollup so lets it follow imports made through the `client.ts` /
// `hooks.ts` barrels to the domain module that owns each name, instead of
// pulling every domain into the entry chunk.
const pureApiModule =
  /\/src\/(?:api\/(?:client|hooks|aiWorkspaceActions)|types\/models)(?:\.ts|\/[^/]+\.ts)$/;

const frameworkModule =
  /\/node_modules\/(?:react|react-dom|scheduler|react-router|react-router-dom|@remix-run\/router|@tanstack\/[^/]+|zustand)\//;

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const sidecarUrl = env.OPC_DEV_SIDECAR_URL ?? "http://127.0.0.1:9876";

  return {
    plugins: [react(), tailwindcss()],
    clearScreen: false,
    build: {
      rollupOptions: {
        output: {
          // The framework changes far less often than the application, so it
          // gets its own cacheable chunk. Icons and xterm stay with the code
          // that uses them.
          manualChunks(id) {
            return frameworkModule.test(id.split("\\").join("/"))
              ? "framework"
              : undefined;
          },
        },
        treeshake: {
          moduleSideEffects: (id) =>
            !pureApiModule.test(id.split("\\").join("/")),
        },
      },
    },
    server: {
      strictPort: true,
      proxy: {
        "/api": {
          target: sidecarUrl,
          changeOrigin: true,
          headers: { Origin: "http://127.0.0.1:1420" },
        },
        "/health": {
          target: sidecarUrl,
          changeOrigin: true,
          headers: { Origin: "http://127.0.0.1:1420" },
        },
      },
    },
    test: {
      environment: "jsdom",
      setupFiles: "./src/test/setup.ts",
      css: true,
    },
  };
});
