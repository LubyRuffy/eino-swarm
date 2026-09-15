import { defineConfig, devices } from "@playwright/test"

// The suite drives the real binary against the scripted offline provider, in a
// throwaway data directory: no network, no keys, and the same HTTP and SSE code
// path the desktop shell uses.
const port = Number(process.env.ZWAI_E2E_PORT ?? 8799)
const dataDir = process.env.ZWAI_E2E_DATA_DIR ?? ".e2e-data"

export default defineConfig({
  testDir: "./e2e",
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
    trace: "retain-on-failure",
  },
  webServer: {
    // The wipe belongs here rather than in globalSetup: Playwright starts the
    // web server first, so a setup hook would delete the directory the server
    // had already opened.
    command: `rm -rf ${dataDir} && go run ../cmd/zwai web --addr 127.0.0.1:${port} --no-open --mock --data-dir ${dataDir}`,
    url: `http://127.0.0.1:${port}/api/meta`,
    // Never reuse: a server left over from a previous run holds an open handle
    // to the database this run just deleted.
    reuseExistingServer: false,
    timeout: 180_000,
    stdout: "pipe",
    stderr: "pipe",
  },
})
