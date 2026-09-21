/** The conversation column the composer sits on. ResizeObserver writes the
 *  box's height here so the transcript can pad its last lines out from under it. */
export const COMPOSER_STAGE_ATTR = "data-composer-stage"

export const COMPOSER_PAD_VAR = "--composer-pad"

export const COMPOSER_FADE_OVERHANG_VAR = "--composer-fade-overhang"

/** Join above the dock, in CSS pixels — not rem. html rem is the
 *  conversation size; a Tailwind `-top-40` used to grow the wash over
 *  the last answer whenever Settings bumped that size. */
export const COMPOSER_FADE_OVERHANG_PX = 96

/** Last line sits this far into the join so it is not on the opaque edge
 *  where the slab meets the fade. Pins stay in the dock; this is air,
 *  not a second cover. */
export const COMPOSER_FADE_AIR_PX = 32

/** Transcript pad for a measured dock. Goal / plan / wait pins live in
 *  that height — growing a % opaque stop over dock+hang was what painted
 *  out the last answer on a compact box and still leaked between chips. */
export function composerPadPx(dockHeight: number): number {
  if (dockHeight <= 0) return 0
  return Math.round(dockHeight) + COMPOSER_FADE_AIR_PX
}

/** Keep the last message readable when the composer is painted on top of it.
 *  Height 0 is a jsdom layout, not a real box — leave the CSS fallback. */
export function syncComposerPad(from: HTMLElement | null, height: number): void {
  const stage = from?.closest(`[${COMPOSER_STAGE_ATTR}]`)
  if (!(stage instanceof HTMLElement)) return
  if (height <= 0) return
  stage.style.setProperty(COMPOSER_PAD_VAR, `${composerPadPx(height)}px`)
  stage.style.setProperty(
    COMPOSER_FADE_OVERHANG_VAR,
    `${COMPOSER_FADE_OVERHANG_PX}px`,
  )
}

export function clearComposerPad(from: HTMLElement | null): void {
  const stage = from?.closest(`[${COMPOSER_STAGE_ATTR}]`)
  if (!(stage instanceof HTMLElement)) return
  stage.style.removeProperty(COMPOSER_PAD_VAR)
  stage.style.removeProperty(COMPOSER_FADE_OVERHANG_VAR)
}

/** Grow the box with the draft. Skip while an IME is composing: `height:auto`
 *  forces a layout and the candidate window jumps on every preedit key.
 *  Overflow stays hidden until the cap — an empty box with padding or a
 *  wrapping placeholder must not paint a scrollbar. */
export const COMPOSER_AREA_MAX_PX = 200

export function resizeComposerArea(
  el: HTMLTextAreaElement | null,
  opts?: { composing?: boolean; maxPx?: number },
): void {
  if (!el || opts?.composing) return
  const maxPx = opts?.maxPx ?? COMPOSER_AREA_MAX_PX
  el.style.height = "auto"
  const needed = el.scrollHeight
  el.style.height = `${Math.min(needed, maxPx)}px`
  el.style.overflowY = needed > maxPx ? "auto" : "hidden"
}
