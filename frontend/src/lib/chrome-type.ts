/** Window chrome type: sidebar, Settings, title bar, composer controls.
 *  Size is the CSS token, not rem, so Settings → Font size scales the
 *  conversation and leaves this quiet. */
export const chromeTypeClass =
  "text-[length:var(--chrome-font-size)] font-normal leading-snug"

/** Goal / plan / wait chips above the composer. One Codex-style row, not a card. */
export const composerPinClass =
  "mb-1.5 rounded-xl bg-muted/40 px-2.5 py-1 text-[length:var(--chrome-font-size)] font-normal leading-snug"
