import { MANAGER_ID, type TranscriptState } from "./transcript"

/** Non-quiet manager rows that are the conversation, not the Agents tab.
 *  Spawn rows used to satisfy this after a roster sidecar landed, so opening
 *  a long /goal stopped paging and showed only "Started …". */
export function visibleManagerBlockCount(state: TranscriptState): number {
  return (state.agents[MANAGER_ID]?.blocks ?? []).filter(
    (b) => !b.quiet && b.kind !== "spawn",
  ).length
}

export function managerHasVisibleBlocks(state: TranscriptState): boolean {
  return visibleManagerBlockCount(state) > 0
}

export function isWelcomePane(input: {
  activeId?: string
  loaded: boolean
  visibleManagerBlocks: number
  running: boolean
  workerCount: number
  historyHasMore: boolean
}): boolean {
  if (!input.activeId) return true
  if (!input.loaded) return false
  // A tail-loaded running turn is often just worker tool rows: the manager's
  // user bubble and spawn rows sit on an older page. Treating "no manager
  // rows" as a blank conversation paints the welcome cards over the
  // transcript, which also hides the history sentinel that would page those
  // rows in — the roster then shows workers next to "What should we work on?".
  if (input.running || input.workerCount > 0 || input.historyHasMore) return false
  return input.visibleManagerBlocks === 0
}
