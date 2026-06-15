import path from "node:path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    css: false,
  },
  build: {
    // Build straight into the Go embed directory so the binary picks it up.
    outDir: "../backend/embed/assets",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      // Proxy API calls to the Go backend during development.
      "/api": "http://localhost:3001",
    },
  },
});
