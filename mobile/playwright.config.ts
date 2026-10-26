import { defineConfig, devices } from "@playwright/test"

const port = Number(process.env.ZWAI_MOBILE_E2E_PORT ?? 4174)
const origin = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  fullyParallel: false,
  workers: 1,
  use: {
    ...devices["Pixel 7"],
    baseURL: origin,
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
      testMatch: ["quoted-goal.spec.ts", "conversation-open.spec.ts", "engine-startup.spec.ts", "generation-interruption.spec.ts", "pc-timeouts.spec.ts", "model-refresh.spec.ts", "composer-layout.spec.ts", "ask-answer.spec.ts", "ask-restart.spec.ts", "agent-roster.spec.ts", "api-error.spec.ts", "unread-completion.spec.ts", "active-unread.spec.ts", "worker-stop-replay.spec.ts", "markdown-highlight.spec.ts", "goal-completion.spec.ts", "goal-pause.spec.ts"],
      use: { ...devices["iPhone 13"], browserName: "webkit", permissions: [] },
    },
  ],
  webServer: {
    command: `npm run build && npx vite preview --host 127.0.0.1 --port ${port} --strictPort`,
    url: origin,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: "pipe",
    stderr: "pipe",
  },
})
