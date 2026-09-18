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

