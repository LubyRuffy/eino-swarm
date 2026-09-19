import { en, type MessageKey } from "./messages-en"
import { zh } from "./messages-zh"

export type Locale = "en" | "zh"

const catalogs: Record<Locale, Record<MessageKey, string>> = { en, zh }

const LOCALE_KEY = "zwai.phone.locale"

export function resolveLocale(language = systemLanguage()): Locale {
  return language.toLowerCase().startsWith("zh") ? "zh" : "en"
}

export function systemLanguage(): string {
  if (typeof navigator === "undefined") return "en"
  return navigator.language || "en"
}

function storageGet(key: string): string | null {
  try {
    if (typeof localStorage === "undefined" || typeof localStorage.getItem !== "function") {
      return null
    }
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function storageSet(key: string, value: string) {
  try {
    if (typeof localStorage === "undefined" || typeof localStorage.setItem !== "function") {
      return
    }
    localStorage.setItem(key, value)
  } catch {
    // jsdom / Node stubs
  }
}

function readStored(): Locale | undefined {
  const v = storageGet(LOCALE_KEY)
  if (v === "en" || v === "zh") return v
  return undefined
}

let current: Locale = readStored() ?? resolveLocale()

export function setLocale(next: Locale) {
  current = next
  storageSet(LOCALE_KEY, next)
}

export function getLocale(): Locale {
  return current
}

export function t(key: MessageKey, vars?: Record<string, string | number>): string {
  let s = catalogs[current][key] || catalogs.en[key] || key
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replaceAll("{" + k + "}", String(v))
    }
  }
  return s
}

export function toggleLocale(): Locale {
  const next: Locale = current === "zh" ? "en" : "zh"
  setLocale(next)
  return next
}

export function localeSwitchLabel(): string {
  return current === "zh" ? t("locale.en") : t("locale.zh")
}
