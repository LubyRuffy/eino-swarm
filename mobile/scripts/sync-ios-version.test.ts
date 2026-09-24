import { describe, expect, it } from "vitest"

import { syncedProjectVersion } from "./sync-ios-version"

describe("syncedProjectVersion", () => {
  it("updates every build configuration from the package version", () => {
    const before = "MARKETING_VERSION = 0.1.12;\nCURRENT_PROJECT_VERSION = 112;\nMARKETING_VERSION = 0.1.12;\nCURRENT_PROJECT_VERSION = 112;"
    const after = syncedProjectVersion(before, "0.1.13")
    expect(after.match(/MARKETING_VERSION = 0\.1\.13;/g)).toHaveLength(2)
    expect(after.match(/CURRENT_PROJECT_VERSION = 113;/g)).toHaveLength(2)
    expect(after).not.toContain("0.1.12")
  })

  it("fails instead of silently shipping a project with no version settings", () => {
    expect(() => syncedProjectVersion("CURRENT_PROJECT_VERSION = 112;", "0.1.13")).toThrow("missing")
  })
})
