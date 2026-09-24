import type { SkillTidyMerge, SkillTidyReport } from "./types"

/** How long the scan line stays current before the review line takes over.
 *  The bar itself is tidyEstimateRatio, not this timer. */
export const TIDY_SCAN_MS = 400

/** The bar stops here until the request actually finishes. A catalog tidy
 *  has no token budget the client can see, so the rest is an estimate. */
export const TIDY_ESTIMATE_CAP = 0.92

const TIDY_ESTIMATE_FLOOR_MS = 12_000
const TIDY_ESTIMATE_MS_PER_SKILL = 8_000

export const TIDY_PROGRESS_STEPS = ["scan", "review"] as const

/** Fraction of the bar to paint while a tidy is still running. Grows with
 *  elapsed time and the catalog size, and never reaches 1. */
export function tidyEstimateRatio(elapsedMs: number, skillCount: number): number {
  const n = Math.max(1, skillCount)
  const expected = Math.max(TIDY_ESTIMATE_FLOOR_MS, n * TIDY_ESTIMATE_MS_PER_SKILL)
  const raw = 1 - Math.exp(-Math.max(0, elapsedMs) / expected)
  return Math.min(TIDY_ESTIMATE_CAP, raw)
}

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
    patched: [],
    merged: [],
    reviewed: false,
  }
}

export function tidyFolded(report?: SkillTidyReport): boolean {
  if (!report) return false
  return (
    report.merged.length > 0 ||
    report.deleted.length > 0 ||
    report.created.length > 0 ||
    report.patched.length > 0
  )
}

export function namesOf(list: string[] | undefined): string[] {
  return Array.isArray(list) ? list.filter((n) => n.trim() !== "") : []
}

export function normalizeTidyReport(raw: Partial<SkillTidyReport> | undefined, scanned = 0): SkillTidyReport {
  const created = namesOf(raw?.created)
  const deleted = namesOf(raw?.deleted)
  const patched = namesOf(raw?.patched)
  const merged = Array.isArray(raw?.merged) ? raw.merged.map(normalizeMerge).filter((m) => m.keep) : []
  const before = num(raw?.before, scanned)
  const after = num(raw?.after, Math.max(0, before - deleted.length + created.length))
  const err = typeof raw?.err === "string" && raw.err.trim() !== "" ? raw.err : undefined
  return {
    scanned: num(raw?.scanned, scanned),
    before,
    after,
    families: num(raw?.families, merged.length),
    unchanged: num(raw?.unchanged, Math.max(0, before - deleted.length - merged.filter((m) => !m.created).length)),
    created,
    deleted,
    patched,
    merged,
    reviewed: raw?.reviewed === true,
    err,
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
