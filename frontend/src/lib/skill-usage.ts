import type { SkillInfo } from "./types"

const DAY_MS = 24 * 60 * 60 * 1000

/** The server's zero time represents a skill that has never been viewed. */
export function skillLastViewed(skill: SkillInfo): number | undefined {
  const value = Date.parse(skill.last_viewed_at ?? "")
  return Number.isFinite(value) && value > 0 ? value : undefined
}

/** An old skill is a review candidate; this never changes or removes it. */
export function skillIsStale(skill: SkillInfo, days: number | undefined, now = Date.now()): boolean {
  if (!days || days <= 0) return false
  const lastUse = skillLastViewed(skill) ?? Date.parse(skill.updated_at)
  return Number.isFinite(lastUse) && now - lastUse >= days * DAY_MS
}
