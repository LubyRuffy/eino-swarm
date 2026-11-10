/** The first number a client sees is the baseline. Only a later change means
 *  the data directory was replaced and the loaded list is stale. */
export function dataGenerationChanged(
  previous: number | null | undefined,
  next: number | null | undefined,
): boolean {
  if (previous == null || next == null) return false
  return previous !== next
}
