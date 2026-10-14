import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: {
    // During `npm run dev`, forward API calls to the Go API.
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
