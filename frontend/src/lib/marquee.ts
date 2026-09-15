/** Whether a live status line is wider than the slot it sits in. */
export function measureOverflow(
  outer: { clientWidth: number },
  sizer: { scrollWidth: number },
): boolean {
  return sizer.scrollWidth > outer.clientWidth + 1
}

/** Seconds for one loop. Longer lines take longer, so the eye can keep up. */
export function marqueeDuration(text: string): string {
  const s = Math.min(40, Math.max(8, Math.round(text.length / 8)))
  return `${s}s`
}
