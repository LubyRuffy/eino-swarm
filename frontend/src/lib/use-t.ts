import { useCallback } from "react"

import {
  applyLocale,
  resolveLocale,
  t,
  type Locale,
  type LocalePref,
  type MessageKey,
  type Vars,
} from "@/lib/i18n"
import { useApp } from "@/store/app"

export type Translate = ((key: MessageKey, vars?: Vars) => string) & {
  locale: Locale
  pref: LocalePref
}

/** Chrome strings for the current language. Subscribes to the store so a
 *  switch re-renders without a reload. */
export function useT(): Translate {
  const pref = useApp((s) => s.locale)
  const locale = resolveLocale(pref)
  const translate = useCallback(
    (key: MessageKey, vars?: Vars) => t(locale, key, vars),
    [locale],
  )
  return Object.assign(translate, { locale, pref })
}

export function toggleLocalePref(pref: LocalePref, language?: string): Locale {
  const current = resolveLocale(pref, language)
  const next: LocalePref = current === "zh" ? "en" : "zh"
  applyLocale(next)
  return next
}
