/** Window chrome type: sidebar, Settings, title bar, composer controls.
 *  Size is the UI token. Content font size must not move this. */
export const chromeTypeClass =
  "text-[length:var(--chrome-font-size)] font-normal leading-snug"

/** Goal / plan / wait chips above the composer. Opaque so transcript
 *  lines cannot bleed through a stack of pins. */
export const composerPinClass =
  "rounded-xl border border-border bg-background px-2.5 py-1 text-[length:var(--chrome-font-size)] font-normal leading-snug"

/** Conversation body. Face and size come from Settings → Content font.
 *  Size is a Tailwind token so it wins over Textarea's text-sm. */
export const contentTypeClass =
  "content-type text-[length:var(--ui-font-size)] leading-6"
