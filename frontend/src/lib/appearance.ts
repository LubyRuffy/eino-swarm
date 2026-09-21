import { normalizeLocalePref, type LocalePref } from "./i18n"

/** Typeface for the window. system is the UI sans stack already in CSS. */
export type FontPref = "system" | "serif" | "mono"

/** Conversation reading size. medium matches --chrome-font-size (Settings).
 *  Window chrome uses --chrome-font-size and does not follow this. */
export type FontSizePref = "small" | "medium" | "large"

/** Conversation column. comfortable is the current max-w-3xl reading width. */
export type ContentWidthPref = "comfortable" | "full"

export interface Appearance {
  font: FontPref
  fontSize: FontSizePref
  contentWidth: ContentWidthPref
}

export interface UISettings {
  locale: LocalePref
  font: FontPref
  font_size: FontSizePref
  content_width: ContentWidthPref
}

export const APPEARANCE_KEY = "zwai.appearance"

export const defaultAppearance = (): Appearance => ({
  font: "system",
  fontSize: "medium",
  contentWidth: "comfortable",
})

/** Stacks stay here so a preference is a token, not a CSS string in config.yaml.
 *  CJK faces ride along because the chrome is bilingual. */
const FONT_STACKS: Record<FontPref, string> = {
  system:
    'ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif',
  serif:
    'ui-serif, "Songti SC", "Noto Serif CJK SC", "Noto Serif SC", Georgia, "Times New Roman", serif',
  mono: 'ui-monospace, SFMono-Regular, "SF Mono", Menlo, "Sarasa Gothic SC", "Noto Sans Mono CJK SC", monospace',
}

const FONT_SIZES: Record<FontSizePref, string> = {
  small: "12px",
  medium: "13px",
  large: "16px",
}

const CONTENT_MAX: Record<ContentWidthPref, string> = {
  comfortable: "48rem",
  full: "none",
}

/** Side inset of the conversation column. full is a hair so text does not
 *  kiss the sidebar; comfortable keeps the current sm:px-8 gutter. */
const CONTENT_GUTTER: Record<ContentWidthPref, string> = {
  comfortable: "2rem",
  full: "1rem",
}

/** Title-bar / ⌘K flip. The stored tokens stay comfortable / full. */
export function toggleContentWidth(pref: ContentWidthPref): ContentWidthPref {
  return pref === "full" ? "comfortable" : "full"
}

export function normalizeFont(value: string | undefined | null): FontPref {
  switch ((value ?? "").trim().toLowerCase()) {
    case "serif":
      return "serif"
    case "mono":
      return "mono"
    default:
      return "system"
  }
}

export function normalizeFontSize(
  value: string | undefined | null,
): FontSizePref {
  switch ((value ?? "").trim().toLowerCase()) {
    case "small":
      return "small"
    case "large":
      return "large"
    default:
      return "medium"
  }
}

export function normalizeContentWidth(
  value: string | undefined | null,
): ContentWidthPref {
  switch ((value ?? "").trim().toLowerCase()) {
    case "full":
      return "full"
    default:
      return "comfortable"
  }
}

export function normalizeAppearance(
  raw?: Partial<{
    font?: string
    font_size?: string
    fontSize?: string
    content_width?: string
    contentWidth?: string
  } | null>,
): Appearance {
  return {
    font: normalizeFont(raw?.font),
    fontSize: normalizeFontSize(raw?.font_size ?? raw?.fontSize),
    contentWidth: normalizeContentWidth(raw?.content_width ?? raw?.contentWidth),
  }
}

/** YAML / API chrome. Strings until normalizeUISettings pins the tokens. */
export type UISettingsInput = {
  locale?: string
  font?: string
  font_size?: string
  content_width?: string
  fontSize?: string
  contentWidth?: string
}

export function normalizeUISettings(
  raw?: UISettingsInput | null,
  locale?: string,
): UISettings {
  const appearance = normalizeAppearance(raw)
  return {
    locale: normalizeLocalePref(locale || raw?.locale),
    font: appearance.font,
    font_size: appearance.fontSize,
    content_width: appearance.contentWidth,
  }
}

export function readAppearance(): Appearance {
  try {
    const raw = localStorage.getItem(APPEARANCE_KEY)
    if (!raw) return defaultAppearance()
    return normalizeAppearance(JSON.parse(raw) as Record<string, string>)
  } catch {
    return defaultAppearance()
  }
}

export function writeAppearance(pref: Appearance): void {
  try {
    localStorage.setItem(
      APPEARANCE_KEY,
      JSON.stringify({
        font: pref.font,
        font_size: pref.fontSize,
        content_width: pref.contentWidth,
      }),
    )
  } catch {
    // A preference is not worth failing to start over.
  }
}

export function applyAppearance(pref: Appearance): void {
  if (typeof document === "undefined") return
  const root = document.documentElement
  root.style.setProperty("--font-sans", FONT_STACKS[pref.font])
  root.style.setProperty("--ui-font-size", FONT_SIZES[pref.fontSize])
  root.style.setProperty("--content-max", CONTENT_MAX[pref.contentWidth])
  root.style.setProperty("--content-gutter", CONTENT_GUTTER[pref.contentWidth])
  // --chrome-font-size lives in CSS. Writing it here would make Font size
  // balloon the sidebar and Settings, which is the cheap look.
  root.dataset.font = pref.font
  root.dataset.fontSize = pref.fontSize
  root.dataset.contentWidth = pref.contentWidth
}
