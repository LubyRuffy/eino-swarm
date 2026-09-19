/** /goal is recorded even when it clears. The banner holds the text; this
 *  line is only the fact that it changed. */
export function goalNotice(text?: string): string {
  return text?.trim() ? "Standing objective set." : "Standing objective cleared."
}

type CompactPayload = {
  auto?: boolean
  phase?: string
  summary?: string
  tokens_before?: number
  tokens_after?: number
}

function parseCompactPayload(text?: string): CompactPayload | undefined {
  const raw = text?.trim() ?? ""
  if (!raw.startsWith("{")) return undefined
  try {
    return JSON.parse(raw) as CompactPayload
  } catch {
    return undefined
  }
}

/** The briefing itself lives in the next turn's prompt, not in this row.
 *  Dumping the JSON payload here would paste a summary the human did not ask
 *  to read in the transcript. Auto-compact must say so, with the token
 *  counts, or a long turn looks like it is still carrying the full prompt. */
export function compactNotice(ev: { text?: string; err?: string }): string {
  if (ev.err) return ev.err
  const p = parseCompactPayload(ev.text)
  if (p) {
    if (p.phase === "start") return "Compressing conversation context…"
    if (p.auto) {
      const before = Number(p.tokens_before) || 0
      const after = Number(p.tokens_after) || 0
      if (before > 0 || after > 0) {
        return `Context compressed (${before} → ${after} tokens). The transcript is unchanged.`
      }
      return "Context compressed. The transcript is unchanged."
    }
  }
  return "Earlier turns were folded into a briefing. The transcript is unchanged."
}

/** Summary later turns will see. Empty while compressing, on failure, or
 *  when an older event never stored one — the row stays a one-liner. */
export function compactBriefing(ev: { text?: string; err?: string }): string {
  if (ev.err) return ""
  const p = parseCompactPayload(ev.text)
  if (!p || p.phase === "start") return ""
  return p.summary?.trim() ?? ""
}

/** Must match engine.goalSessionWrapSteer. Older sessions recorded this as a
 *  leftover wrap; the reducer hides it so a replayed cut does not look like
 *  human guidance. Match the stable prefix so a wording tweak does not
 *  bring the bubble back. */
export const GOAL_SESSION_WRAP_STEER =
  "This work session is ending. Summarize current progress. Do not start new long-running work. Call complete_goal only if the standing objective is actually satisfied. Call block_goal if this session retried the same obstacle as the previous one and meaningful progress needs the human or an external change. Otherwise stop this turn without asking the human."

export function isGoalSessionWrapSteer(text?: string): boolean {
  const t = (text ?? "").trim().replace(/^\[steer\]\s*/, "")
  return t.startsWith("This work session is ending.")
}

/** A recoverable ChatModel failure (truncated tool JSON, 429, a dropped
 *  stream). JSON `{attempt,cap}` is for Trace, not this row. */
export function modelRetryNotice(_text?: string): string {
  return "Retrying after a model error."
}

/** Auto-continue budget, or a human stop. JSON is for Trace, not this row.
 *  The resume sentence stays on this line: a /goal session folds the rest. */
export function goalCappedNotice(text?: string): string {
  try {
    const body = JSON.parse(text ?? "") as { reason?: string }
    if (body.reason === "interrupted") {
      return "Standing objective paused. Press Start on the goal to keep going."
    }
  } catch {
    /* cap payload is {auto_turns,cap}; ignore junk */
  }
  return "Stopped auto-continuing: the standing objective is still open. Press Start on the goal to keep going."
}

/** No-progress hold. The event may still carry the older protocol line. */
export function goalIdleNotice(_text?: string): string {
  return "Stopped auto-continuing: the last continuation made no progress. Press Start on the goal to keep going."
}

/** Lifecycle notices that must stay outside a folded /goal session. A cap
 *  or idle hold used to vanish behind Worked-for, so the pause looked like a crash. */
export function isGoalHoldNotice(text?: string): boolean {
  const s = (text ?? "").trim()
  return (
    s.startsWith("Stopped auto-continuing:") ||
    s.startsWith("Standing objective paused.") ||
    s.startsWith("Standing objective blocked:")
  )
}

/** A forced /goal session end. The JSON payload is for Trace, not a chat row. */
export function goalSessionNotice(text?: string): string {
  let reason = ""
  try {
    const body = JSON.parse(text ?? "") as { reason?: string }
    reason = body.reason ?? ""
  } catch {
    reason = ""
  }
  if (reason === "time") return "Work session ended after the time cap."
  if (reason === "iterations") return "Work session ended after the tool-round cap."
  return "Work session ended."
}
