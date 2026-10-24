import type { TranscriptState } from "./transcript"
import type { SwarmEvent } from "./types"

export type RecoveryEvent = SwarmEvent & { kind: "desktop_recovery" | "engine_restore" }

/** Recovery diagnostics have event identity but never agent/task lifecycle. */
export function recordRecovery(state: TranscriptState, event: SwarmEvent): boolean {
  if (event.kind !== "desktop_recovery" && event.kind !== "engine_restore") return false
  const events = state.recoveryEvents ?? []
  if (event.seq > 0 && events.some((saved) => saved.thread_id === event.thread_id && saved.seq === event.seq)) return true
  state.recoveryEvents = [...events, { ...event, kind: event.kind }]
  return true
}
