import type { SkillInfo } from "./types"

const DAY_MS = 24 * 60 * 60 * 1000

/** The server's zero time represents a skill that has never been viewed. */
export function skillLastViewed(skill: SkillInfo): number | undefined {
  const value = Date.parse(skill.last_viewed_at ?? "")
  return Number.isFinite(value) && value > 0 ? value : undefined
}

/** A character count short enough for a meta line: 940, 1.2K, 18K. */
export function formatChars(n: number | undefined): string {
  const v = Math.max(0, n ?? 0)
  if (v < 1000) return String(v)
  return v < 10_000 ? `${(v / 1000).toFixed(1)}K` : `${Math.round(v / 1000)}K`
}

/** An old skill is a review candidate; this never changes or removes it. */
export function skillIsStale(skill: SkillInfo, days: number | undefined, now = Date.now()): boolean {
  if (!days || days <= 0) return false
  const lastUse = skillLastViewed(skill) ?? Date.parse(skill.updated_at)
  return Number.isFinite(lastUse) && now - lastUse >= days * DAY_MS
}
