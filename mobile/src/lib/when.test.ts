import { describe, expect, it } from "vitest"

import { setLocale } from "./i18n"
import { timeAgo } from "./when"

const NOW = Date.parse("2026-09-22T12:00:00Z")

function at(ms: number): string {
  return new Date(NOW - ms).toISOString()
}

describe("timeAgo", () => {
  it("counts an inbox row's age in the unit a glance can use", () => {
    setLocale("en")
    expect(timeAgo(at(5_000), NOW)).toBe("just now")
    expect(timeAgo(at(9 * 60_000), NOW)).toBe("9m ago")
    expect(timeAgo(at(5 * 3_600_000), NOW)).toBe("5h ago")
    expect(timeAgo(at(3 * 86_400_000), NOW)).toBe("3d ago")
  })

  // A PC whose clock runs behind the phone would otherwise read "-2m ago".
  it("reads a future timestamp as now rather than a negative age", () => {
    setLocale("en")
    expect(timeAgo(new Date(NOW + 90_000).toISOString(), NOW)).toBe("just now")
  })

  it("falls back to a date once the row is older than a week", () => {
    setLocale("en")
    const out = timeAgo(at(30 * 86_400_000), NOW)
    expect(out).not.toContain("ago")
    expect(out).toMatch(/Aug/)
  })

  it("is empty when the host sent no timestamp, so no row shows NaN", () => {
    expect(timeAgo(undefined, NOW)).toBe("")
    expect(timeAgo("not-a-date", NOW)).toBe("")
  })

  it("follows the picked locale", () => {
    setLocale("zh")
    expect(timeAgo(at(9 * 60_000), NOW)).toBe("9 分钟前")
    setLocale("en")
  })
})
