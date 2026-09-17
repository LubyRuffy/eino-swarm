/** /goal is recorded even when it clears. The banner holds the text; this
 *  line is only the fact that it changed. */
export function goalNotice(text?: string): string {
  return text?.trim() ? "Standing objective set." : "Standing objective cleared."
}

/** The briefing itself lives in the next turn's prompt, not in this row.
 *  Dumping the JSON payload here would paste a summary the human did not ask
 *  to read in the transcript. Auto-compact must say so, with the token
 *  counts, or a long turn looks like it is still carrying the full prompt. */
export function compactNotice(ev: { text?: string; err?: string }): string {
  if (ev.err) return ev.err
  const raw = ev.text?.trim() ?? ""
  if (raw.startsWith("{")) {
    try {
      const p = JSON.parse(raw) as {
        auto?: boolean
        phase?: string
        tokens_before?: number
        tokens_after?: number
      }
      if (p.phase === "start") return "Compressing conversation context…"
      if (p.auto) {
        const before = Number(p.tokens_before) || 0
        const after = Number(p.tokens_after) || 0
        if (before > 0 || after > 0) {
          return `Context compressed (${before} → ${after} tokens). The transcript is unchanged.`
        }
        return "Context compressed. The transcript is unchanged."
      }
    } catch {
      /* payload is for the next prompt, not this row */
    }
  }
  return "Earlier turns were folded into a briefing. The transcript is unchanged."
}

/** Must match engine.goalSessionWrapSteer. Older sessions recorded this as a
 *  steer event; the reducer hides it so a session cut does not look like
 *  human guidance. Match the stable prefix so a wording tweak does not
 *  bring the bubble back. */
export const GOAL_SESSION_WRAP_STEER =
  "This work session is ending. Summarize current progress. Do not start new long-running work. Call complete_goal only if the standing objective is actually satisfied. Call block_goal if this session retried the same obstacle as the previous one and meaningful progress needs the human or an external change. Otherwise stop this turn without asking the human."

export function isGoalSessionWrapSteer(text?: string): boolean {
  const t = (text ?? "").trim().replace(/^\[steer\]\s*/, "")
  return t.startsWith("This work session is ending.")
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
