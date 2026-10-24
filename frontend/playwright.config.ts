import { defineConfig, devices } from "@playwright/test"

// The suite drives the real binary against the scripted offline provider, in a
// throwaway data directory. The opt-in API-error spec uses a keyless loopback
// endpoint through the real SDK; the default suite stays on the offline script.
const reasoningFields = process.env.ZWAI_E2E_REASONING_FIELDS === "1"
const port = Number(process.env.ZWAI_E2E_PORT ?? 8799)
const dataDir = process.env.ZWAI_E2E_DATA_DIR ?? ".e2e-data"
const scheduledAck = process.env.ZWAI_MOCK_SCHEDULE_ACK === "always" ? "always" : "once"
const modelAPIError = process.env.ZWAI_E2E_MODEL_API_ERROR === "1"
const workerRoster = process.env.ZWAI_E2E_WORKER_ROSTER === "1"
const scheduleAnswer = process.env.ZWAI_E2E_SCHEDULE_ANSWER === "1"
const workerCompletion = process.env.ZWAI_E2E_WORKER_COMPLETION === "1"
const goalSchedule = process.env.ZWAI_E2E_GOAL_SCHEDULE === "1"
const humanWakePolicy = process.env.ZWAI_E2E_HUMAN_WAKE_POLICY === "1"
const memoryReview = process.env.ZWAI_E2E_MEMORY_REVIEW === "1"
const builtinTools = process.env.ZWAI_E2E_BUILTIN_TOOLS === "1"
const workerOwnership = process.env.ZWAI_E2E_SCHEDULE_WORKER_OWNERSHIP === "1"
const generationInterruption = process.env.ZWAI_E2E_GENERATION_INTERRUPTION === "1"
const loopbackModel = reasoningFields || workerCompletion || scheduleAnswer || modelAPIError || workerRoster || goalSchedule || humanWakePolicy || memoryReview || builtinTools || workerOwnership || generationInterruption

export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/engine-presence.ts",
  testMatch: reasoningFields ? "reasoning-fields.spec.ts" : workerCompletion ? "worker-completion.spec.ts" : scheduleAnswer ? "schedule-answer.spec.ts" : generationInterruption ? "generation-interruption.spec.ts" : workerOwnership ? "schedule-worker-ownership.spec.ts" : builtinTools ? "builtin-tools.spec.ts" : memoryReview ? "memory-review.spec.ts" : modelAPIError ? "api-error.spec.ts" : workerRoster ? "worker-roster-history.spec.ts" : goalSchedule ? "goal-schedule.spec.ts" : humanWakePolicy ? "human-wake-policy.spec.ts" : undefined,
  timeout: 90_000,
  expect: { timeout: 20_000 },
  fullyParallel: false,
  workers: 1,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "line" : [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    baseURL: `http://127.0.0.1:${port}`,
    viewport: { width: 1440, height: 900 },
    locale: "en-US",
    trace: "retain-on-failure",
  },
  webServer: {
    // The wipe belongs here rather than in globalSetup: Playwright starts the
    // web server first, so a setup hook would delete the directory the server
    // had already opened.
    // Workers pause so wait_agents stays pending long enough to Steer.
    command: `rm -rf ${dataDir} && ZWAI_MOCK_WORKER_DELAY_MS=1500 ZWAI_MOCK_SCHEDULE_ACK=${scheduledAck} go run ../cmd/zwai web --addr 127.0.0.1:${port} --no-open ${loopbackModel ? "" : "--mock"} --data-dir ${dataDir}`,
    env: loopbackModel ? { OPENAI_BASE_URL: "", OPENAI_API_KEY: "", OPENAI_MODEL: "" } : undefined,
    url: `http://127.0.0.1:${port}/api/meta`,
    // Never reuse: a server left over from a previous run holds an open handle
    // to the database this run just deleted.
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: "pipe",
    stderr: "pipe",
  },
})
