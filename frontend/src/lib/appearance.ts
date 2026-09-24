import { writeChromeListDensity } from "./chrome-density"
import { normalizeLocalePref, type LocalePref } from "./i18n"

/** Chrome typeface. system is the UI sans stack already in CSS. */
export type FontPref = "system" | "serif" | "mono"

/** Conversation / code typeface. ui follows the chrome face. */
export type FacePref = "ui" | FontPref

/** Absolute size tokens. */
export type FontSizeToken = "small" | "medium" | "large"

/** Conversation reading size. ui follows the chrome size so they match. */
export type FontSizePref = "ui" | FontSizeToken

/** Code size. content follows the conversation. */
export type CodeFontSizePref = "ui" | "content" | FontSizeToken

/** Conversation column. comfortable is the current max-w-3xl reading width. */
export type ContentWidthPref = "comfortable" | "full"

/** Transcript chrome. user folds thinking and tools; developer keeps every row. */
export type TranscriptModePref = "user" | "developer"

/** Named color set. zwai is the current chrome; fofa is the console palette. */
export type PalettePref = "zwai" | "fofa"

export interface Appearance {
  font: FontPref
  uiFontSize: FontSizeToken
  contentFont: FacePref
  fontSize: FontSizePref
  codeFont: FacePref
  codeFontSize: CodeFontSizePref
  contentWidth: ContentWidthPref
  transcriptMode: TranscriptModePref
  palette: PalettePref
}

export interface UISettings {
  locale: LocalePref
  font: FontPref
  ui_font_size: FontSizeToken
  content_font: FacePref
  font_size: FontSizePref
  code_font: FacePref
  code_font_size: CodeFontSizePref
  content_width: ContentWidthPref
  transcript_mode: TranscriptModePref
  palette: PalettePref
}

export const APPEARANCE_KEY = "zwai.appearance"

export const defaultAppearance = (): Appearance => ({
  font: "system",
  uiFontSize: "medium",
  contentFont: "ui",
  fontSize: "ui",
  codeFont: "mono",
  codeFontSize: "content",
  contentWidth: "comfortable",
  transcriptMode: "user",
  palette: "zwai",
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

const FONT_SIZES: Record<FontSizeToken, string> = {
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

/** App menu / ⌘K flip. The stored tokens stay comfortable / full. */
export function toggleContentWidth(pref: ContentWidthPref): ContentWidthPref {
  return pref === "full" ? "comfortable" : "full"
}

/** App menu flip between the compact transcript and every tool row. */
export function toggleTranscriptMode(
  pref: TranscriptModePref,
): TranscriptModePref {
  return pref === "developer" ? "user" : "developer"
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

export function normalizeFace(
  value: string | undefined | null,
  blank: FacePref,
): FacePref {
  switch ((value ?? "").trim().toLowerCase()) {
    case "system":
      return "system"
    case "serif":
      return "serif"
    case "mono":
      return "mono"
    case "ui":
      return "ui"
    default:
      return blank
  }
}

export function normalizeFontSizeToken(
  value: string | undefined | null,
  fallback: FontSizeToken = "medium",
): FontSizeToken {
  switch ((value ?? "").trim().toLowerCase()) {
    case "small":
      return "small"
    case "large":
      return "large"
    case "medium":
      return "medium"
    default:
      return fallback
  }
}

export function normalizeFontSize(
  value: string | undefined | null,
): FontSizePref {
  const raw = (value ?? "").trim().toLowerCase()
  if (raw === "ui") return "ui"
  if (raw === "small" || raw === "medium" || raw === "large") return raw
  return "medium"
}

export function normalizeCodeFontSize(
  value: string | undefined | null,
): CodeFontSizePref {
  const raw = (value ?? "").trim().toLowerCase()
  if (raw === "ui") return "ui"
  if (raw === "small" || raw === "medium" || raw === "large") return raw
  return "content"
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

export function normalizeTranscriptMode(
  value: string | undefined | null,
): TranscriptModePref {
  switch ((value ?? "").trim().toLowerCase()) {
    case "developer":
      return "developer"
    default:
      return "user"
  }
}

export function normalizePalette(
  value: string | undefined | null,
): PalettePref {
  switch ((value ?? "").trim().toLowerCase()) {
    case "fofa":
      return "fofa"
    default:
      return "zwai"
  }
}

export type AppearanceInput = Partial<{
  font?: string
  ui_font_size?: string
  uiFontSize?: string
  content_font?: string
  contentFont?: string
  font_size?: string
  fontSize?: string
  code_font?: string
  codeFont?: string
  code_font_size?: string
  codeFontSize?: string
  content_width?: string
  contentWidth?: string
  transcript_mode?: string
  transcriptMode?: string
  palette?: string
} | null>

export function normalizeAppearance(raw?: AppearanceInput): Appearance {
  const d = defaultAppearance()
  const uiFontSize = raw?.ui_font_size ?? raw?.uiFontSize
  const contentFont = raw?.content_font ?? raw?.contentFont
  const codeFont = raw?.code_font ?? raw?.codeFont
  const codeFontSize = raw?.code_font_size ?? raw?.codeFontSize
  return {
    font: normalizeFont(raw?.font),
    uiFontSize: uiFontSize
      ? normalizeFontSizeToken(uiFontSize)
      : d.uiFontSize,
    contentFont: contentFont
      ? normalizeFace(contentFont, "ui")
      : d.contentFont,
    fontSize: raw?.font_size ?? raw?.fontSize
      ? normalizeFontSize(raw?.font_size ?? raw?.fontSize)
      : d.fontSize,
    codeFont: codeFont ? normalizeFace(codeFont, "mono") : d.codeFont,
    codeFontSize: codeFontSize
      ? normalizeCodeFontSize(codeFontSize)
      : d.codeFontSize,
    contentWidth: normalizeContentWidth(raw?.content_width ?? raw?.contentWidth),
    transcriptMode: normalizeTranscriptMode(
      raw?.transcript_mode ?? raw?.transcriptMode,
    ),
    palette: normalizePalette(raw?.palette),
  }
}

/** YAML / API chrome. Strings until normalizeUISettings pins the tokens. */
export type UISettingsInput = AppearanceInput & { locale?: string }

export function appearanceToUI(pref: Appearance, locale: LocalePref): UISettings {
  return {
    locale,
    font: pref.font,
    ui_font_size: pref.uiFontSize,
    content_font: pref.contentFont,
    font_size: pref.fontSize,
    code_font: pref.codeFont,
    code_font_size: pref.codeFontSize,
    content_width: pref.contentWidth,
    transcript_mode: pref.transcriptMode,
    palette: pref.palette,
  }
}

export function normalizeUISettings(
  raw?: UISettingsInput | null,
  locale?: string,
): UISettings {
  return appearanceToUI(
    normalizeAppearance(raw),
    normalizeLocalePref(locale || raw?.locale),
  )
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
        ui_font_size: pref.uiFontSize,
        content_font: pref.contentFont,
        font_size: pref.fontSize,
        code_font: pref.codeFont,
        code_font_size: pref.codeFontSize,
        content_width: pref.contentWidth,
        transcript_mode: pref.transcriptMode,
        palette: pref.palette,
      }),
    )
  } catch {
    // A preference is not worth failing to start over.
  }
}

function stackFor(face: FacePref, ui: FontPref): string {
  return FONT_STACKS[face === "ui" ? ui : face]
}

function pxForContent(pref: Appearance): string {
  if (pref.fontSize === "ui") return FONT_SIZES[pref.uiFontSize]
  return FONT_SIZES[pref.fontSize]
}

function pxForCode(pref: Appearance): string {
  if (pref.codeFontSize === "ui") return FONT_SIZES[pref.uiFontSize]
  if (pref.codeFontSize === "content") return pxForContent(pref)
  return FONT_SIZES[pref.codeFontSize]
}

export function applyAppearance(pref: Appearance): void {
  if (typeof document === "undefined") return
  const root = document.documentElement
  root.style.setProperty("--font-sans", FONT_STACKS[pref.font])
  root.style.setProperty("--font-content", stackFor(pref.contentFont, pref.font))
  root.style.setProperty("--font-mono", stackFor(pref.codeFont, pref.font))
  const chromePx = FONT_SIZES[pref.uiFontSize]
  root.style.setProperty("--chrome-font-size", chromePx)
  writeChromeListDensity(root.style, Number.parseFloat(chromePx))
  root.style.setProperty("--ui-font-size", pxForContent(pref))
  root.style.setProperty("--code-font-size", pxForCode(pref))
  root.style.setProperty("--content-max", CONTENT_MAX[pref.contentWidth])
  root.style.setProperty("--content-gutter", CONTENT_GUTTER[pref.contentWidth])
  root.dataset.font = pref.font
  root.dataset.uiFontSize = pref.uiFontSize
  root.dataset.contentFont = pref.contentFont
  root.dataset.fontSize = pref.fontSize
  root.dataset.codeFont = pref.codeFont
  root.dataset.codeFontSize = pref.codeFontSize
  root.dataset.contentWidth = pref.contentWidth
  root.dataset.transcriptMode = pref.transcriptMode
  root.dataset.palette = pref.palette
}
