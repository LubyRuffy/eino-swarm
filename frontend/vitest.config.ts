import path from "node:path"
import { defineConfig } from "vitest/config"

// Tests live in their own config because vitest ships its own copy of vite,
// and mixing the two type trees in vite.config.ts makes `tsc -b` unhappy.
// esbuild handles the JSX here using tsconfig's react-jsx setting, so no
// plugin is needed.
export default defineConfig({
  resolve: {
    alias: { "@": path.resolve(__dirname, "./src") },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
