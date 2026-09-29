import type { Block, TranscriptState } from "./transcript"

const MANAGER_ID = "manager"

/** Event-seq the engine stored on `steer_retracted`. JSON `{seq}` is the
 *  live payload; a bare integer is accepted so an older row still hides. */
export function parseSteerRetractSeq(text?: string): number {
  const raw = (text ?? "").trim()
  if (!raw) return 0
  try {
    const parsed = JSON.parse(raw) as { seq?: unknown }
    const n = Number(parsed.seq)
    if (Number.isInteger(n) && n > 0) return n
  } catch {
    // not JSON — try a plain integer below
  }
  const n = Number.parseInt(raw, 10)
  return Number.isInteger(n) && n > 0 ? n : 0
}

export function isRetractedSteer(state: TranscriptState, seq: number): boolean {
  return seq > 0 && Boolean(state.retractedSteers?.includes(seq))
}

/** `steer_revised` payload. `text` is required so a bare `{seq}` cannot
 *  wipe a caption. An empty string is a real clear (image-only steer). */
export function parseSteerRevision(text?: string): { seq: number; text: string } | undefined {
  const raw = (text ?? "").trim()
  if (!raw) return
  try {
    const parsed = JSON.parse(raw) as { seq?: unknown; text?: unknown }
    const seq = Number(parsed.seq)
    if (!Number.isInteger(seq) || seq <= 0 || typeof parsed.text !== "string") return
    return { seq, text: parsed.text }
  } catch {
    return
  }
}

/** Remember the latest caption and paint it onto a bubble that is already
 *  on screen. A revision that arrives before its steer (history paging)
 *  still applies when that steer is folded. */
export function rememberSteerRevision(state: TranscriptState, seq: number, text: string): void {
  if (seq <= 0) return
  state.steerRevisions = { ...(state.steerRevisions ?? {}), [seq]: text }
  const agent = state.agents[MANAGER_ID]
  if (!agent) return
  let changed = false
  const blocks = agent.blocks.map((b) => {
    if (b.kind !== "steer" || b.seq !== seq || b.text === text) return b
    changed = true
    return { ...b, text }
  })
  if (changed) state.agents[MANAGER_ID] = { ...agent, blocks }
}

export function revisedSteerText(state: TranscriptState, seq: number, text: string): string {
  const revised = state.steerRevisions?.[seq]
  return revised !== undefined ? revised : text
}

/** Drop revisions at or after a rewind cut. Earlier bubbles stay edited. */
export function steerRevisionsBefore(
  revisions: Record<number, string> | undefined,
  fromSeq: number,
): Record<number, string> | undefined {
  if (!revisions) return
  const next: Record<number, string> = {}
  let kept = false
  for (const [key, text] of Object.entries(revisions)) {
    const seq = Number(key)
    if (seq > 0 && seq < fromSeq) {
      next[seq] = text
      kept = true
    }
  }
  return kept ? next : undefined
}

/** Remember the seq so a later-loaded `steer` (history paging) stays hidden,
 *  and drop the bubble if it is already on the manager. */
export function dropRetractedSteer(state: TranscriptState, seq: number): void {
  if (seq <= 0) return
  const retracted = state.retractedSteers ?? []
  if (!retracted.includes(seq)) state.retractedSteers = [...retracted, seq]
  const agent = state.agents[MANAGER_ID]
  if (!agent) return
  const blocks = agent.blocks.filter((b) => !(b.kind === "steer" && b.seq === seq))
  if (blocks.length === agent.blocks.length) return
  state.agents[MANAGER_ID] = { ...agent, blocks }
}

/** The manager started another round. Tool results patch a row that already
 *  exists, so they do not count — that is the current round finishing, not
 *  the next one reading the inbox. */
function isModelRound(b: Block): boolean {
  return b.kind === "reasoning" || b.kind === "answer" || b.kind === "tool"
}

/** Pull unread steering out of the turn body. Steering is queued for the next
 *  model call; leaving the bubble where the user typed it parks it above
 *  "Working for…" and reads as already applied. A later round on the same
 *  turn consumes it and it stays in chronological order. A finished turn
 *  keeps the event order so history does not jump. */
export function splitQueuedSteers(
  blocks: Block[],
  running: boolean,
): { body: Block[]; queued: Block[] } {
  if (!running || blocks.length === 0) {
    return { body: blocks, queued: [] }
  }
  const turnId = blocks[blocks.length - 1].turnId
  let lastRound = -1
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i]
    if (b.turnId === turnId && isModelRound(b)) lastRound = i
  }
  const body: Block[] = []
  const queued: Block[] = []
  for (let i = 0; i < blocks.length; i++) {
    const b = blocks[i]
    if (b.kind === "steer" && b.turnId === turnId && i > lastRound) {
      queued.push(b)
    } else {
      body.push(b)
    }
  }
  return { body, queued }
}

