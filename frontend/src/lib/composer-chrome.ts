/** The conversation column the composer sits on. ResizeObserver writes the
 *  box's height here so the transcript can pad its last lines out from under it. */
export const COMPOSER_STAGE_ATTR = "data-composer-stage"

export const COMPOSER_PAD_VAR = "--composer-pad"

/** Keep the last message readable when the composer is painted on top of it.
 *  Height 0 is a jsdom layout, not a real box — leave the CSS fallback. */
export function syncComposerPad(from: HTMLElement | null, height: number): void {
  const stage = from?.closest(`[${COMPOSER_STAGE_ATTR}]`)
  if (!(stage instanceof HTMLElement)) return
  if (height <= 0) return
  stage.style.setProperty(COMPOSER_PAD_VAR, `${Math.round(height)}px`)
}

export function clearComposerPad(from: HTMLElement | null): void {
  const stage = from?.closest(`[${COMPOSER_STAGE_ATTR}]`)
  if (!(stage instanceof HTMLElement)) return
  stage.style.removeProperty(COMPOSER_PAD_VAR)
}
