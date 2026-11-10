/** A generation the phone has not seen yet is the baseline. A later change
 *  means rows already loaded may have been deleted, so the list starts over. */
export function rosterGenerationReset(
  seen: number | null,
  next: number | null | undefined,
): { seen: number | null; reset: boolean } {
  if (typeof next !== "number") return { seen, reset: false }
  if (seen == null) return { seen: next, reset: false }
  if (seen === next) return { seen, reset: false }
  return { seen: next, reset: true }
}
