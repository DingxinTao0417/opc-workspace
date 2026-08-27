import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const sidecarUrl = env.OPC_DEV_SIDECAR_URL ?? "http://127.0.0.1:9876";

  return {
    plugins: [react(), tailwindcss()],
    clearScreen: false,
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
