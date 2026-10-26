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
