import { defineConfig, devices } from "@playwright/test"

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  use: {
    ...devices["Pixel 7"],
    baseURL: "http://127.0.0.1:4174",
    locale: "zh-CN",
  },
  webServer: {
    command: "npm run build && npm run preview",
    url: "http://127.0.0.1:4174",
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: "pipe",
    stderr: "pipe",
  },
})
