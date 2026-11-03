export interface ComposerPrediction {
  text: string
  turnId: string
}

const CLEAR_KINDS = new Set([
  "user_message",
  "goal_continued",
  "plan_implemented",
  "schedule_fired",
  "resumed",
  "restart_resumed",
])

/** Missing means on. Only an explicit false stops suggestions. */
export function predictionsEnabled(value: boolean | undefined): boolean {
  return value !== false
}

/** The latest completed turn owns the placeholder. An older call must not
 *  replace it, and a decline of this turn removes whatever was showing. */
export function nextComposerPrediction(
  turns: { id: string }[],
  current: ComposerPrediction | undefined,
  ev: { turn_id: string; text?: string; err?: string },
): ComposerPrediction | undefined {
  const latest = turns[turns.length - 1]?.id
  if (!ev.turn_id || ev.turn_id !== latest) return current
  const text = ev.text?.trim() ?? ""
  if (ev.err || !text) return undefined
  return { text, turnId: ev.turn_id }
}

export function applyComposerPredictionEvent(
  state: { turns: { id: string }[]; prediction?: ComposerPrediction },
  ev: { kind: string; turn_id: string; text?: string; err?: string },
): void {
  if (CLEAR_KINDS.has(ev.kind)) {
    delete state.prediction
    return
  }
  if (ev.kind !== "composer_prediction") return
  const accepted = nextComposerPrediction(state.turns, state.prediction, ev)
  if (accepted) state.prediction = accepted
  else delete state.prediction
}

/** Tab inserts only while the composer is empty. A typed draft wins. */
export function predictionToInsert(text: string, prediction: string | undefined): string | null {
  const next = prediction?.trim() ?? ""
  if (!next || text.trim() !== "") return null
  return next
}
