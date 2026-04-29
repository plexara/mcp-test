import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// Vite serves the SPA from /portal/ in production. The Go backend embeds
// dist/ via go:embed and falls back to index.html for unknown paths so
// react-router can take over.
export default defineConfig({
  base: "/portal/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  server: {
    proxy: {
      "/api":           { target: "http://localhost:8080", changeOrigin: true },
      "/portal/auth":   { target: "http://localhost:8080", changeOrigin: true },
      "/.well-known":   { target: "http://localhost:8080", changeOrigin: true },
      "/healthz":       { target: "http://localhost:8080", changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
