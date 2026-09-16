/** The IME "Process" key. CJK input methods send this for the keypress that
 *  confirms composition, including leftover Latin the user wants to keep. */
export const IME_KEYCODE = 229

/** Whether this keydown should send the composer draft.
 *
 *  Enter sends; Shift+Enter is a newline. An IME confirmation is neither:
 *  Chinese IMEs use Enter to commit leftover Latin instead of converting it.
 *  WebKit (the desktop window) fires compositionend *before* that Enter, so
 *  `isComposing` is already false — callers must also pass whether composition
 *  is still live in this frame. */
export function enterSendsMessage(
  e: Pick<KeyboardEvent, "key" | "shiftKey" | "isComposing" | "keyCode">,
  composing: boolean,
): boolean {
  if (e.key !== "Enter" || e.shiftKey) return false
  if (composing || e.isComposing || e.keyCode === IME_KEYCODE) return false
  return true
}

/** Run `settled` after this frame's input events. WebKit delivers the
 *  confirming Enter in the same frame as compositionend; clearing the
 *  composing flag synchronously would send the draft. */
export function afterImeSettles(settled: () => void): () => void {
  const id = window.requestAnimationFrame(settled)
  return () => window.cancelAnimationFrame(id)
}
