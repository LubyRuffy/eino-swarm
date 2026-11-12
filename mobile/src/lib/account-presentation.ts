export type AccountPresentation = "official" | "full"

const KEY = "zwai.phone.account.presentation"
export const ACCOUNT_PRESENTATION_EVENT = "zwai.phone.account.presentation"

export function loadAccountPresentation(): AccountPresentation {
  try {
    return localStorage.getItem(KEY) === "full" ? "full" : "official"
  } catch {
    return "official"
  }
}

export function saveAccountPresentation(value: AccountPresentation) {
  localStorage.setItem(KEY, value)
  window.dispatchEvent(new Event(ACCOUNT_PRESENTATION_EVENT))
}
