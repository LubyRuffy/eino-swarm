import { describe, expect, it } from "vitest"

import { skillIsStale, skillLastViewed } from "./skill-usage"
import type { SkillInfo } from "./types"

const now = Date.parse("2026-10-23T12:00:00Z")
const day = 86_400_000
const skill = (patch: Partial<SkillInfo> = {}): SkillInfo => ({
  name: "procedure", description: "When the stored checks apply", updated_at: new Date(now).toISOString(), ...patch,
})

describe("skill usage", () => {
  it("treats absent, damaged and server zero times as never viewed", () => {
    for (const value of [undefined, "", "invalid", "0001-01-01T00:00:00Z"]) {
      expect(skillLastViewed(skill({ last_viewed_at: value }))).toBeUndefined()
    }
    expect(skillLastViewed(skill({ last_viewed_at: new Date(now).toISOString() }))).toBe(now)
  })

  it("includes the exact configured stale boundary and excludes a recently viewed skill", () => {
    const candidate = skill({ updated_at: new Date(now - 10 * day).toISOString() })
    expect(skillIsStale(candidate, 10, now - 1)).toBe(false)
    expect(skillIsStale(candidate, 10, now)).toBe(true)
    expect(skillIsStale({ ...candidate, last_viewed_at: new Date(now - day).toISOString() }, 10, now)).toBe(false)
  })

  it("uses modification time only for skills with no actual views", () => {
    const candidate = skill({ last_viewed_at: "0001-01-01T00:00:00Z", updated_at: new Date(now - 10 * day).toISOString() })
    expect(skillIsStale(candidate, 10, now)).toBe(true)
    expect(skillIsStale({ ...candidate, updated_at: "bad" }, 10, now)).toBe(false)
    expect(skillIsStale(candidate, undefined, now)).toBe(false)
    expect(skillIsStale(candidate, 0, now)).toBe(false)
    expect(skillIsStale(candidate, -1, now)).toBe(false)
  })
})
