import type { TranscriptState } from "./transcript"
import type { SwarmEvent } from "./types"

export type DesktopRecoveryEvent = SwarmEvent & { kind: "desktop_recovery" }

/** Interface diagnostics have event identity but never agent/task lifecycle. */
export function recordDesktopRecovery(state: TranscriptState, event: SwarmEvent): boolean {
  if (event.kind !== "desktop_recovery") return false
  const events = state.desktopRecoveryEvents ?? []
  if (event.seq > 0 && events.some((saved) => saved.thread_id === event.thread_id && saved.seq === event.seq)) return true
  state.desktopRecoveryEvents = [...events, { ...event, kind: "desktop_recovery" }]
  return true
}
