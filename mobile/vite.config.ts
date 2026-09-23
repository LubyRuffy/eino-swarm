import { readFileSync } from "node:fs"
import path from "node:path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

function shellVersion(): string {
  const pkg = JSON.parse(readFileSync(new URL("./package.json", import.meta.url), "utf8")) as {
    version?: unknown
  }
  return typeof pkg.version === "string" ? pkg.version : ""
}

export default defineConfig({
  define: {
    __ZWAI_WEB_VERSION__: JSON.stringify(shellVersion()),
  },
  plugins: [react()],
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false },
})
