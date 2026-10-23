import type { Block } from "./transcript"

/** Human Start records the old turn's resume before the new turn's continuation.
 *  Keep both receipts in state and Trace; only their adjacent chat rows merge. */
export function visibleGoalNotices(blocks: Block[]): Block[] {
  const merged = new Set<Block>()
  let previous: Block | undefined
  for (const block of blocks) {
    if (block.quiet) continue
    if (previous?.goalNoticeKind === "goal_resumed" && previous.role === "human" &&
        block.goalNoticeKind === "goal_continued" && block.turnId &&
        previous.turnId !== block.turnId && previous.seq > 0 && block.seq > previous.seq) {
      merged.add(previous)
    }
    previous = block
  }
  return merged.size ? blocks.filter((block) => !merged.has(block)) : blocks
}
