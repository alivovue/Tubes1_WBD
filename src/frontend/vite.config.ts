import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  // Preserve the existing /public/ logo URL. Go serves public/ in production.
  publicDir: false,
  server: {
    proxy: {
      "/api": "http://127.0.0.1:3000",
      "/public": "http://127.0.0.1:3000",
    },
  },
  test: { environment: "jsdom", setupFiles: ["./app/test-setup.ts"] },
});
