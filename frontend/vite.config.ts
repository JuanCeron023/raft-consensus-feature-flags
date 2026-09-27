import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Build output goes to the directory the Go server serves (web/static),
// and dev requests to /api and /ws are proxied to the local backend.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../web/static",
    emptyOutDir: true,
  },
  server: {
    proxy: {
      "/api": "http://localhost:9001",
      "/ws": { target: "ws://localhost:9001", ws: true },
    },
  },
});
