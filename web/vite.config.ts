import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // The desktop app's Preview may hand the dev server a free port through PORT.
    port: Number((globalThis as { process?: { env?: Record<string, string | undefined> } }).process?.env?.PORT) || 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8080", changeOrigin: false },
      "/guide": { target: "http://127.0.0.1:8080", changeOrigin: false },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
  },
});
