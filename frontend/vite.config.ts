import path from "node:path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    port: 5173,
    // In dev the Go server runs separately; proxying keeps the app on one
    // origin so the event stream and uploads behave exactly as they do in a
    // build.
    proxy: {
      "/api": {
        target: process.env.ZWAI_BACKEND ?? "http://127.0.0.1:8787",
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // The bundle is committed so `go run ./cmd/zwai desktop` works on a fresh
    // clone; sourcemaps would triple the diff for no benefit to users.
    sourcemap: false,
  },
})
