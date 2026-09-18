import { MANAGER_ID, type TranscriptState } from "@/lib/transcript"
import type { Followup } from "@/lib/types"

/** Bumped on every follow-up mutation so an in-flight GET cannot resurrect
 *  a row the user just steered or a turn that just consumed it. */
let generation = 0

export function bumpFollowups(): number {
  return ++generation
}

export function isFollowupGeneration(token: number): boolean {
  return token === generation
}

export function dropMatchingFollowups(items: Followup[], text: string): Followup[] {
  const want = text.trim()
  if (!want) return items
  return items.filter((f) => f.text.trim() !== want)
}

/** The user bubble of the in-flight turn. A leftover Enter after that
 *  send must not enqueue the same words as a follow-up. */
export function liveTurnUserText(
  status: { turn_id?: string },
  transcript: Pick<TranscriptState, "turns" | "agents">,
): string {
  const turnId = status.turn_id
  if (turnId) {
    const row = transcript.turns.find((t) => t.id === turnId)
    if (row?.userText.trim()) return row.userText.trim()
  }
  const blocks = transcript.agents[MANAGER_ID]?.blocks ?? []
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (blocks[i].kind === "user" && blocks[i].text.trim()) {
      return blocks[i].text.trim()
    }
  }
  return ""
}
