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
