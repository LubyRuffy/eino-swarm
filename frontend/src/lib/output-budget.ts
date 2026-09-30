/** Wire text of swarm.errOutputBudget. The desktop transcript matches it
 *  exactly so a stored event still grows a settings link after replay.
 *  Change signal.go and this string together. */
export const OUTPUT_BUDGET_ERROR =
  "the model used its whole output budget before it produced an answer"

/** data-settings-key of the swarm row that edits max_completion_tokens. */
export const MAX_COMPLETION_SETTINGS_KEY = "max_completion_tokens"

export function isOutputBudgetError(text: string | undefined): boolean {
  return (text ?? "").trim() === OUTPUT_BUDGET_ERROR
}

export type BudgetRetryMessage = { text: string; seq: number }

type BudgetBlock = {
  kind: string
  text: string
  seq: number
  turnId: string
  images?: readonly unknown[]
}

/** The user message a retry resends. Same turn as the failure, and only
 *  when that message still has text or an image — an empty rewind is rejected. */
export function budgetRetryMessages(
  blocks: readonly BudgetBlock[],
): Map<string, BudgetRetryMessage> {
  const out = new Map<string, BudgetRetryMessage>()
  for (const b of blocks) {
    if (b.kind !== "user" || !(b.seq > 0) || !b.turnId) continue
    if (!b.text.trim() && !(b.images && b.images.length > 0)) continue
    out.set(b.turnId, { text: b.text, seq: b.seq })
  }
  return out
}
