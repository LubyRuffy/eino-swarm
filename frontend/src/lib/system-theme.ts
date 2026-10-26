import type { Theme } from "@/store/app"

/** Keep the system preference live without changing the saved theme choice. */
export function observeSystemTheme(theme: Theme, apply: (theme: Theme) => void) {
  if (theme !== "system" || !window.matchMedia) return
  const media = window.matchMedia("(prefers-color-scheme: dark)")
  const update = () => apply(theme)
  media.addEventListener("change", update)
  return () => media.removeEventListener("change", update)
}
