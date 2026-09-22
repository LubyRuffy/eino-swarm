import { getLocale, t } from "./i18n"

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const WEEK = 7 * DAY

/** An inbox row answers "how stale is this", not "at what instant". A clock
 *  that runs ahead of the PC must read as now, never as a negative age. */
export function timeAgo(iso: string | undefined, now = Date.now()): string {
  const at = iso ? Date.parse(iso) : NaN
  if (!Number.isFinite(at)) return ""
  const age = now - at
  if (age < MINUTE) return t("when.now")
  if (age < HOUR) return t("when.minutes", { n: Math.floor(age / MINUTE) })
  if (age < DAY) return t("when.hours", { n: Math.floor(age / HOUR) })
  if (age < WEEK) return t("when.days", { n: Math.floor(age / DAY) })
  return onDate(at)
}

function onDate(at: number): string {
  const tag = getLocale() === "zh" ? "zh-CN" : "en-US"
  try {
    return new Date(at).toLocaleDateString(tag, { month: "short", day: "numeric" })
  } catch {
    return new Date(at).toISOString().slice(0, 10)
  }
}
