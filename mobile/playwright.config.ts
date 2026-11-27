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
    // Legacy pairing/model scenarios exercise Local mode explicitly. Account
    // admission scenarios remove this preference before loading the product.
    storageState: { cookies: [], origins: [{ origin, localStorage: [{ name: "zwai.phone.mode", value: "local" }] }] },
    permissions: ["camera"],
    launchOptions: {
      args: ["--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream"],
    },
  },
  projects: [
    { name: "phone" },
    {
      name: "iphone-layout",
      testMatch: ["direct-image-generation.spec.ts", "paired-image-generation.spec.ts", "transcript-images.spec.ts", "speech-stream.spec.ts", "task-panels.spec.ts", "work-records.spec.ts", "selection-actions.spec.ts", "local-model-sources.spec.ts", "phone-account-mode.spec.ts", "composer-prediction.spec.ts", "markdown-mermaid.spec.ts", "project-management.spec.ts", "iteration-limit.spec.ts", "file-preview-controls.spec.ts", "gateway-account.spec.ts", "cloud-web-tools.spec.ts", "file-download.spec.ts", "reasoning-fold.spec.ts", "quoted-goal.spec.ts", "conversation-open.spec.ts", "engine-startup.spec.ts", "generation-interruption.spec.ts", "pc-timeouts.spec.ts", "model-refresh.spec.ts", "composer-layout.spec.ts", "composer-commands.spec.ts", "ask-answer.spec.ts", "ask-restart.spec.ts", "agent-roster.spec.ts", "api-error.spec.ts", "unread-completion.spec.ts", "schedule-findings-source.spec.ts", "active-unread.spec.ts", "worker-stop-replay.spec.ts", "markdown-highlight.spec.ts", "markdown-table.spec.ts", "goal-completion.spec.ts", "goal-pause.spec.ts", "goal-mode.spec.ts"],
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
