import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: { dedupe: ["react", "react-dom"] },
  // Preserve the existing /public/ logo URL. Go serves public/ in production.
  publicDir: false,
  server: {
    port: 5174,
    proxy: {
      "/api": "http://127.0.0.1:3001",
      "/public": "http://127.0.0.1:3001",
    },
  },
});
