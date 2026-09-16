import { en } from "./messages-en"
import { zh } from "./messages-zh"

export type Locale = "en" | "zh"
export type LocalePref = "system" | Locale
export type MessageKey = keyof typeof en
export type Vars = Record<string, string | number>

export const LOCALE_KEY = "zwai.locale"

const catalogs: Record<Locale, Record<MessageKey, string>> = { en, zh }

const NOTICE_EXACT: Record<string, MessageKey> = {
  "Standing objective set.": "notice.goalSet",
  "Standing objective cleared.": "notice.goalCleared",
  "Standing objective completed.": "notice.goalCompleted",
  "Continuing the standing objective.": "notice.goalContinued",
  "Stopped auto-continuing: the standing objective is still open.":
    "notice.goalCapped",
  "Standing objective blocked: progress needs you or an external change.":
    "notice.goalBlocked",
  "Standing objective updated.": "notice.goalEdited",
  "Resuming the standing objective.": "notice.goalResumed",
  "Earlier turns were folded into a briefing. The transcript is unchanged.":
    "notice.compacted",
  "Review finished — nothing new to keep.": "notice.reviewQuiet",
  "Review finished.": "notice.reviewDone",
  "Memory updated.": "notice.memoryUpdated",
}

const REVIEW_FAIL_PREFIX = "Memory review failed: "

/** Chrome preference: a pinned language, or follow the browser. Junk becomes
 *  system so a hand-edited config cannot blank the UI. */
export function normalizeLocalePref(value: string | undefined | null): LocalePref {
  switch ((value ?? "").trim().toLowerCase()) {
    case "en":
      return "en"
    case "zh":
      return "zh"
    default:
      return "system"
  }
}

/** Which dictionary to paint. `system` is Chinese only when the browser
 *  language is zh*; everything else is English so a French install is not a
 *  surprise half-translation. */
export function resolveLocale(
  pref: LocalePref,
  language = systemLanguage(),
): Locale {
  if (pref === "en" || pref === "zh") return pref
  return language.toLowerCase().startsWith("zh") ? "zh" : "en"
}

export function systemLanguage(): string {
  if (typeof navigator === "undefined") return "en"
  return navigator.language || "en"
}

export function readLocalePref(): LocalePref {
  try {
    return normalizeLocalePref(localStorage.getItem(LOCALE_KEY))
  } catch {
    return "system"
  }
}

export function writeLocalePref(pref: LocalePref): void {
  try {
    localStorage.setItem(LOCALE_KEY, pref)
  } catch {
    // A preference is not worth failing to start over.
  }
}

export function htmlLang(locale: Locale): string {
  return locale === "zh" ? "zh-CN" : "en"
}

export function applyLocale(pref: LocalePref): Locale {
  const locale = resolveLocale(pref)
  if (typeof document !== "undefined") {
    document.documentElement.lang = htmlLang(locale)
  }
  return locale
}

export function t(locale: Locale, key: MessageKey, vars?: Vars): string {
  let out = catalogs[locale][key] || en[key]
  if (!vars) return out
  for (const [name, value] of Object.entries(vars)) {
    out = out.replaceAll(`{${name}}`, String(value))
  }
  return out
}

/** Transcript chrome the reducer still emits in English. Model text and
 *  unknown notices pass through so a translation table cannot swallow them. */
export function localizeNotice(text: string, locale: Locale): string {
  const key = NOTICE_EXACT[text]
  if (key) return t(locale, key)
  if (text.startsWith(REVIEW_FAIL_PREFIX)) {
    return t(locale, "notice.reviewFailed", {
      err: text.slice(REVIEW_FAIL_PREFIX.length),
    })
  }
  return text
}

export function annotationLabelFor(count: number, locale: Locale): string {
  if (count <= 0) return ""
  if (count === 1) return t(locale, "quote.chipOne")
  return t(locale, "quote.chipMany", { n: count })
}

export function findCountLabelFor(
  index: number,
  total: number,
  query: string,
  locale: Locale,
): string {
  if (!query.trim()) return ""
  if (total <= 0) return t(locale, "find.none")
  return t(locale, "find.count", { current: index + 1, total })
}

export function contextHintFor(chars: number, budget: number, locale: Locale): string | undefined {
  if (!(budget > 0) || !(chars >= 0)) return undefined
  const pct = Math.max(0, Math.round((100 * chars) / budget))
  return t(locale, "slash.full", { pct })
}

const DAY_KEYS = {
  today: "time.today",
  yesterday: "time.yesterday",
  week: "time.week",
  month: "time.month",
  earlier: "time.earlier",
} as const satisfies Record<string, MessageKey>

export function dayLabel(
  key: keyof typeof DAY_KEYS,
  locale: Locale,
): string {
  return t(locale, DAY_KEYS[key])
}
