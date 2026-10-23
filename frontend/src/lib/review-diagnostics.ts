import type { Locale } from "./i18n"
import type { ReviewAttempt, ReviewOutcome, ReviewRefusal } from "./types"

const codes = ["evidence", "numeric", "capacity", "validation", "conflict", "storage"] as const
const stops = ["completed", "iteration_limit", "proposal_missing", "error"] as const
const statuses = ["accepted", "refused", "error"] as const
const labels = {
  en: {
    evidence: "Evidence reference did not pass validation",
    numeric: "Number not supported by the cited evidence",
    capacity: "Notes capacity exceeded",
    validation: "Proposal did not pass validation",
    conflict: "Memory changed during review",
    storage: "Memory could not be written",
  },
  zh: {
    evidence: "证据引用未通过校验",
    numeric: "数字缺少所引证据支持",
    capacity: "记忆容量不足",
    validation: "提案未通过校验",
    conflict: "复盘期间记忆发生变化",
    storage: "记忆写入失败",
  },
}

function record(raw: unknown): Record<string, unknown> | undefined {
  return raw && typeof raw === "object" && !Array.isArray(raw) ? raw as Record<string, unknown> : undefined
}
function integer(raw: unknown, min = 0): number | undefined {
  return typeof raw === "number" && Number.isSafeInteger(raw) && raw >= min ? raw : undefined
}
function sequences(raw: unknown): number[] | undefined {
  return Array.isArray(raw) ? raw.filter((n): n is number => integer(n) !== undefined && n > 0).slice(0, 16) : undefined
}
function member<T extends string>(raw: unknown, values: readonly T[]): T | undefined {
  return typeof raw === "string" && values.includes(raw as T) ? raw as T : undefined
}

/** Older events contain free-form reasons. Classify them without reflecting
 *  their quotes or provider bodies back into the chat. */
function legacyCode(reason: string): NonNullable<ReviewRefusal["code"]> {
  if (/^memory: a note is \d+ characters; the per-note cap is \d+/.test(reason)) return "validation"
  if (/over budget|capacity|over_by|exceeds the limit|entry is too long/.test(reason)) return "capacity"
  if (/number|numeric/.test(reason)) return "numeric"
  if (/evidence|quote|source/.test(reason)) return "evidence"
  if (/conflict|revision/.test(reason)) return "conflict"
  return "validation"
}

export function parseReviewDiagnostics(raw: unknown): Partial<ReviewOutcome> {
  const body = record(raw) ?? {}
  const refused = Array.isArray(body.refused) ? body.refused.slice(-32).flatMap((item) => {
    const r = record(item)
    if (!r || integer(r.index, -1) === undefined || typeof r.reason !== "string") return []
    const oldCapacity = /memory is at (\d+)\/(\d+) characters; this write exceeds the limit by (\d+)/.exec(r.reason)
    return [{
      index: r.index as number, reason: r.reason,
      code: member(r.code, codes), attempt: integer(r.attempt, 1), sources: sequences(r.sources),
      usage: typeof r.usage === "string" && /^\d+\/\d+$/.test(r.usage) ? r.usage : oldCapacity ? `${oldCapacity[1]}/${oldCapacity[2]}` : undefined,
      over_by: integer(r.over_by) ?? (oldCapacity ? integer(Number(oldCapacity[3])) : undefined),
    }]
  }) : undefined
  const attempts: ReviewAttempt[] | undefined = Array.isArray(body.attempts) ? body.attempts.slice(-32).flatMap((item) => {
    const r = record(item)
    const status = member(r?.status, statuses)
    if (!r || integer(r.number, 1) === undefined || !status) return []
    const operations = Array.isArray(r.operations) ? r.operations.slice(0, 32).flatMap((item) => {
      const op = record(item)
      return op && typeof op.target === "string" && typeof op.action === "string"
        ? [{ target: ["memory", "skill_manage"].includes(op.target) ? op.target : "unknown", action: ["add", "replace", "remove", "create", "patch", "merge", "delete", "promote", "demote"].includes(op.action) ? op.action : "unknown", sources: sequences(op.sources) }] : []
    }) : []
    return [{ number: r.number as number, status, operations }]
  }) : undefined
  return { stop_reason: member(body.stop_reason, stops), attempt_count: integer(body.attempt_count), refused, attempts }
}

export function reviewFailure(outcome: ReviewOutcome | undefined, locale: Locale): string | undefined {
  if (!outcome || (!outcome.err && (!outcome.stop_reason || outcome.stop_reason === "completed"))) return undefined
  const zh = locale === "zh"
  const lines: string[] = []
  if (outcome.stop_reason === "iteration_limit") {
    lines.push(zh ? "记忆复盘失败：修复轮次已耗尽。" : "Memory review failed: repair round limit reached.")
  } else if (outcome.stop_reason === "proposal_missing") {
    lines.push(zh ? "记忆复盘失败：未提交完整提案。" : "Memory review failed: no complete proposal was submitted.")
  } else {
    lines.push(zh ? "记忆复盘失败。请打开本回合 Trace 查看详情。" : "Memory review failed. Open the turn Trace for details.")
  }
  if (outcome.attempt_count !== undefined) {
    lines.push(zh ? `已提交 ${outcome.attempt_count} 次提案。` : `${outcome.attempt_count} attempts submitted.`)
  }
  // Keep the latest capacity feedback alongside each distinct rejection class;
  // a long retry history stays in Trace rather than flooding the conversation.
  const latest = new Map<NonNullable<ReviewRefusal["code"]>, ReviewRefusal>()
  for (const refusal of outcome.refused ?? []) latest.set(refusal.code ?? legacyCode(refusal.reason), refusal)
  for (const [code, refusal] of latest) {
    let line = labels[locale][code]
    if (code === "capacity") {
      if (refusal.usage) line += ` (${refusal.usage})`
      if (refusal.over_by !== undefined) line += zh ? `，超出 ${refusal.over_by} 字符` : `, ${refusal.over_by} characters over`
    }
    lines.push(`${line}${zh ? "。" : "."}`)
  }
  if (!Object.values(outcome.notes ?? {}).some((n) => n > 0)) lines.push(zh ? "本次笔记未保存。" : "Notes were not saved.")
  if (outcome.skills?.length) lines.push(zh ? "已完成的技能变更已保存。" : "Skill changes were saved.")
  return lines.join("\n")
}

/** The trace retains only operation kinds and source positions. Legacy free-form
 *  rejection text can contain an old note, quote or provider secret. */
export function reviewTraceMetadata(outcome: ReviewOutcome): string | undefined {
  if (!outcome.attempts?.length && !outcome.refused?.length && !outcome.stop_reason && !outcome.stale_candidates?.length) return undefined
  return JSON.stringify({
    stale_candidates: outcome.stale_candidates,
    stop_reason: outcome.stop_reason,
    attempt_count: outcome.attempt_count,
    attempts: outcome.attempts,
    refused: outcome.refused?.map((r) => ({
      index: r.index, code: r.code ?? legacyCode(r.reason), attempt: r.attempt,
      sources: r.sources, usage: r.usage, over_by: r.over_by,
    })),
  }, null, 2)
}
