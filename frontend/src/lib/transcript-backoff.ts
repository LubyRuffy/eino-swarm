export interface ModelBackoffNotice {
  text: string
  cooldownId?: string
}

/** Classifier recovery and service cooldowns have separate owners and clocks. */
export function modelBackoffNotice(text?: string): ModelBackoffNotice | undefined {
  try {
    const body = JSON.parse(text ?? "") as { wait_ms?: unknown; scope?: unknown; code?: unknown; candidate_id?: unknown; cooldown_id?: unknown } | null
    if (body?.scope === "schedule_answer" || typeof body?.candidate_id === "string") {
      if (body?.code === "classification_deferred") {
        return { text: "Answer verification is unavailable. The unpublished answer is retained." }
      }
      if (typeof body.wait_ms === "number" && Number.isFinite(body.wait_ms) && body.wait_ms > 0) {
        return { text: `Waiting ${Math.ceil(body.wait_ms / 1000)}s before checking the unpublished answer again.` }
      }
      return { text: "Waiting to check the unpublished answer again." }
    }
    // A probe has no known remaining wait. Its other callers must not add a
    // second generic row to the same cooldown's timed explanation.
    if (body?.wait_ms === 0) return
    if (typeof body?.wait_ms === "number" && Number.isFinite(body.wait_ms) && body.wait_ms > 0) {
      return {
        text: `Waiting ${Math.ceil(body.wait_ms / 1000)}s before retrying the model request.`,
        cooldownId: typeof body.cooldown_id === "string" && body.cooldown_id ? body.cooldown_id : undefined,
      }
    }
  } catch {
    // Old or malformed events still explain the pause without leaking JSON.
  }
  return { text: "Waiting before retrying the model request." }
}

const invalidDetails = ["missing_message", "json", "trailing", "claim_list", "claim_meaning", "claim_shape", "unspecified_scope_quote", "scope_quote", "schedule_id", "quote_not_in_candidate", "next_run_at"] as const
const deferredDetails = ["unavailable", "invalid", "attempts"] as const

type InvalidDetail = typeof invalidDetails[number]
type DeferredDetail = typeof deferredDetails[number]
export interface ScheduleAnswerBackoff {
  code?: "classification_unavailable" | "classification_invalid" | "classification_deferred"
  detail?: InvalidDetail | DeferredDetail
  attempt?: number
}

/** Retain only closed diagnostics; model output and private candidate text never enter UI state. */
export function scheduleAnswerBackoff(text?: string): ScheduleAnswerBackoff | undefined {
  try {
    const body = JSON.parse(text ?? "")
    if (!body || (body.scope !== "schedule_answer" && typeof body.candidate_id !== "string")) return
    const result: ScheduleAnswerBackoff = {}
    if (["classification_unavailable", "classification_invalid", "classification_deferred"].includes(body.code)) result.code = body.code
    if (Number.isSafeInteger(body.attempt) && body.attempt > 0) result.attempt = body.attempt
    if (result.code === "classification_invalid" && invalidDetails.includes(body.detail)) result.detail = body.detail
    if (result.code === "classification_deferred" && deferredDetails.includes(body.detail)) result.detail = body.detail
    return result
  } catch { return undefined }
}

export function scheduleAnswerBackoffReason(context: ScheduleAnswerBackoff) {
  if (context.code === "classification_invalid" && invalidDetails.includes(context.detail as InvalidDetail)) {
    return `backoff.classification_invalid.${context.detail as InvalidDetail}` as const
  }
  if (context.code === "classification_deferred" && deferredDetails.includes(context.detail as DeferredDetail)) {
    return `backoff.classification_deferred.${context.detail as DeferredDetail}` as const
  }
  return context.code ? `backoff.${context.code}` as const : undefined
}
