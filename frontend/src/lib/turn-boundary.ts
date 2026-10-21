import type { Block, TurnState } from "./transcript"
import { isOmittedBlock } from "./work-fold"

export interface TurnSlice {
  turnId: string
  blocks: Block[]
  afterCompletion: boolean
  showFooter: boolean
}

/** A review can arrive after another turn finished. Keep its log position,
 *  but do not give post-turn feedback another completion or a session fold. */
export function turnSlices(blocks: Block[], turns: TurnState[]): TurnSlice[] {
  const byId = new Map(turns.map((turn) => [turn.id, turn]))
  const slices: TurnSlice[] = []
  const completion = new Map<string, TurnSlice>()
  for (const block of blocks) {
    if (block.quiet || isOmittedBlock(block)) continue
    const turn = byId.get(block.turnId)
    const ended = Boolean(turn?.endedAt && turn.status !== "running")
    const afterCompletion = ended && (turn?.completedSeq
      ? block.seq > turn.completedSeq
      : Date.parse(block.at) > Date.parse(turn?.endedAt ?? ""))
    let slice = slices.at(-1)
    if (!slice || slice.turnId !== block.turnId || slice.afterCompletion !== afterCompletion) {
      slice = { turnId: block.turnId, blocks: [], afterCompletion, showFooter: false }
      slices.push(slice)
    }
    slice.blocks.push(block)
    if (ended && !afterCompletion && !turn?.quiet) {
      const previous = completion.get(block.turnId)
      if (previous) previous.showFooter = false
      slice.showFooter = true
      completion.set(block.turnId, slice)
    }
  }
  return slices
}
