import { readFileSync } from "node:fs"
import path from "node:path"
import { defineConfig } from "vitest/config"

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
  resolve: {
    dedupe: ["react", "react-dom"],
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  server: {
    fs: { allow: [path.resolve(__dirname, "..")] },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}", "native-project.test.ts", "ios-scene-lifecycle.test.ts", "scripts/**/*.test.ts"],
  },
})
