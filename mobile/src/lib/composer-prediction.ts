import type { ComposerPrediction } from "../../../frontend/src/lib/composer-prediction"
import type { RemoteEvent, ThreadDetail } from "./rpc"

const resetKinds = new Set(["user_message", "turn", "goal_continued", "plan_implemented", "schedule_fired", "resumed", "restart_resumed"])

// Derive from the same durable, identity-merged events as the phone transcript.
// Suggestions remain outside chat blocks and never become sendable input until
// the human explicitly inserts one into an empty composer.
export function phonePrediction(events: RemoteEvent[], detail: ThreadDetail, enabled = true): ComposerPrediction | undefined {
  if (!enabled || detail.running || detail.waiting) return undefined
  const snapshot = detail.turns?.at(-1)
  let latest = snapshot?.id ?? ""
  let completed = snapshot?.status === "done"
  let prediction: ComposerPrediction | undefined
  let sawSnapshotTurn = !snapshot
  for (const event of [...events].sort((a, b) => a.seq - b.seq)) {
    if (event.thread_id !== detail.id || event.seq <= 0) continue
    if (event.turn_id === snapshot?.id && event.kind !== "composer_prediction") sawSnapshotTurn = true
    if (resetKinds.has(event.kind)) {
      if (event.turn_id) latest = event.turn_id
      completed = false
      prediction = undefined
    }
    if ((event.kind === "done" || event.kind === "error") && (!event.agent_id || event.agent_id === "manager") && event.turn_id && (!latest || latest === event.turn_id)) {
      latest = event.turn_id
      completed = event.kind === "done"
      if (!completed) prediction = undefined
    }
    if (event.kind === "composer_prediction" && event.turn_id === latest && completed) {
      const text = event.text.trim()
      prediction = text && !event.err ? { text, turnId: latest } : undefined
    }
  }
  // An older page alone cannot supersede a newer detail snapshot.
  return completed && (sawSnapshotTurn || latest === snapshot?.id) ? prediction : undefined
}
