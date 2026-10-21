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
    permissions: ["camera"],
    launchOptions: {
      args: ["--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream"],
    },
  },
  projects: [
    { name: "phone" },
    {
      name: "iphone-layout",
      testMatch: ["model-refresh.spec.ts", "composer-layout.spec.ts", "ask-answer.spec.ts", "agent-roster.spec.ts", "api-error.spec.ts", "unread-completion.spec.ts", "active-unread.spec.ts", "worker-stop-replay.spec.ts"],
      use: { ...devices["iPhone 13"], browserName: "webkit", permissions: [] },
    },
  ],
  webServer: {
    command: "npm run build && npm run preview",
    url: "http://127.0.0.1:4174",
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: "pipe",
    stderr: "pipe",
  },
})
