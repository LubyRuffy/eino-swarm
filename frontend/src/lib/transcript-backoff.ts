export interface ModelBackoffNotice {
  text: string
  cooldownId?: string
}

/** Classifier recovery and service cooldowns have separate owners and clocks. */
export function modelBackoffNotice(text?: string): ModelBackoffNotice | undefined {
  try {
    const body = JSON.parse(text ?? "") as { wait_ms?: unknown; scope?: unknown; code?: unknown; candidate_id?: unknown; cooldown_id?: unknown; detail?: unknown; last_failure?: { code?: unknown } } | null
    if (body?.scope === "schedule_answer" || typeof body?.candidate_id === "string") {
      if (body?.code === "classification_deferred") {
        const failure = classificationFailureCode(body.last_failure?.code)
          ?? (body.detail === "invalid" ? "classification_invalid" : body.detail === "unavailable" ? "classification_unavailable" : undefined)
        if (failure === "classification_invalid") {
          return { text: "The verification response was invalid. The unpublished answer is retained." }
        }
        if (failure === "classification_unavailable") {
          return { text: "The verification request failed. The unpublished answer is retained." }
        }
        return { text: "Answer verification was deferred. The unpublished answer is retained." }
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

const invalidDetails = ["missing_message", "json", "trailing", "claim_list", "claim_meaning", "claim_shape", "unspecified_scope_quote", "scope_quote", "schedule_id", "quote_not_in_candidate", "next_run_at", "decision_response"] as const
const deferredDetails = ["unavailable", "invalid", "attempts"] as const

type InvalidDetail = typeof invalidDetails[number]
type DeferredDetail = typeof deferredDetails[number]
type ClassificationFailureCode = "classification_unavailable" | "classification_invalid"

function classificationFailureCode(code: unknown): ClassificationFailureCode | undefined {
  return code === "classification_unavailable" || code === "classification_invalid" ? code : undefined
}

export interface ScheduleAnswerBackoff {
  code?: ClassificationFailureCode | "classification_deferred"
  detail?: InvalidDetail | DeferredDetail
  attempt?: number
  lastFailure?: { code: ClassificationFailureCode; detail?: InvalidDetail }
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
    if (result.code === "classification_deferred") {
      if (deferredDetails.includes(body.detail)) result.detail = body.detail
      const code = classificationFailureCode(body.last_failure?.code)
      if (code) {
        result.lastFailure = { code }
        if (code === "classification_invalid" && invalidDetails.includes(body.last_failure.detail)) {
          result.lastFailure.detail = body.last_failure.detail
        }
      }
    }
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
