import { writeFileSync } from "node:fs"
import path from "node:path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

function keepEmbedSentinel() {
  return {
    name: "keep-embed-sentinel",
    closeBundle() {
      // emptyOutDir wipes this; go:embed all:dist needs the directory to exist
      writeFileSync(path.resolve(__dirname, "dist/.gitkeep"), "")
    },
  }
}

export default defineConfig({
  plugins: [react(), keepEmbedSentinel()],
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
    // Sourcemaps would triple the artefact size for no benefit to users.
    sourcemap: false,
  },
})
