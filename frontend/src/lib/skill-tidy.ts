import type { SkillTidyMerge, SkillTidyReport } from "./types"

/** How long each tidy progress step stays on screen. A fold is usually
 *  milliseconds; holding the steps is the only way a click shows a process. */
export const TIDY_STEP_MS = 240

export const TIDY_PROGRESS_STEPS = ["scan", "group", "fold"] as const

export type TidyProgressStep = (typeof TIDY_PROGRESS_STEPS)[number]

export function emptyTidyReport(scanned = 0): SkillTidyReport {
  return {
    scanned,
    before: scanned,
    after: scanned,
    families: 0,
    unchanged: scanned,
    created: [],
    deleted: [],
    merged: [],
  }
}

export function tidyFolded(report?: SkillTidyReport): boolean {
  if (!report) return false
  return (
    report.merged.length > 0 ||
    report.deleted.length > 0 ||
    report.created.length > 0
  )
}

export function namesOf(list: string[] | undefined): string[] {
  return Array.isArray(list) ? list.filter((n) => n.trim() !== "") : []
}

export function normalizeTidyReport(raw: Partial<SkillTidyReport> | undefined, scanned = 0): SkillTidyReport {
  const created = namesOf(raw?.created)
  const deleted = namesOf(raw?.deleted)
  const merged = Array.isArray(raw?.merged) ? raw.merged.map(normalizeMerge).filter((m) => m.keep) : []
  const before = num(raw?.before, scanned)
  const after = num(raw?.after, Math.max(0, before - deleted.length + created.length))
  return {
    scanned: num(raw?.scanned, scanned),
    before,
    after,
    families: num(raw?.families, merged.length),
    unchanged: num(raw?.unchanged, Math.max(0, before - deleted.length - merged.filter((m) => !m.created).length)),
    created,
    deleted,
    merged,
  }
}

export function joinNames(names: string[]): string {
  return names.join(", ")
}

function normalizeMerge(raw: SkillTidyMerge): SkillTidyMerge {
  return {
    keep: typeof raw?.keep === "string" ? raw.keep : "",
    dropped: namesOf(raw?.dropped),
    created: Boolean(raw?.created),
  }
}

function num(value: unknown, fallback: number): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback
}
