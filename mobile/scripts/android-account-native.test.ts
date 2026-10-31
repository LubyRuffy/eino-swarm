import { spawnSync } from "node:child_process"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { expect, it } from "vitest"

it("runs the Android evidence privacy regressions through the Mobile test entry", () => {
  const directory = path.dirname(fileURLToPath(import.meta.url))
  const run = spawnSync("python3", ["-B", "-m", "unittest", "discover", "-s", directory, "-p", "test_android_account_native.py"], {
    encoding: "utf8", timeout: 10_000,
  })
  expect(run.error).toBeUndefined()
  expect(run.status, run.stderr).toBe(0)
  expect(run.stderr).toMatch(/Ran [1-9]\d* tests?/)
})
