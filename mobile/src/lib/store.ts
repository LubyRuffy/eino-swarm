import { b64urlToBytes, bytesToB64url } from "./bytes"
import { fromPrivate, generateIdentity, type Identity } from "./crypto"

const ID_KEY = "zwai.remote.identity"
const LINK_KEY = "zwai.remote.link"
const LAST_KEY = "zwai.remote.lastThread"

export type SavedLink = {
  hubURL: string
  ticket: string
  hostPub: string
  sessionID: string
  fingerprint: string
}

export function loadOrCreateIdentity(): Identity {
  const raw = localStorage.getItem(ID_KEY)
  if (raw) {
    try {
      return fromPrivate(b64urlToBytes(raw))
    } catch {
      // mint below
    }
  }
  const id = generateIdentity()
  localStorage.setItem(ID_KEY, bytesToB64url(id.priv))
  return id
}

export function loadSavedLink(): SavedLink | null {
  const raw = localStorage.getItem(LINK_KEY)
  if (!raw) return null
  try {
    const v = JSON.parse(raw) as SavedLink
    if (!v.hubURL || !v.ticket || !v.hostPub || !v.sessionID) return null
    return v
  } catch {
    return null
  }
}

export function saveLink(link: SavedLink) {
  localStorage.setItem(LINK_KEY, JSON.stringify(link))
}

export function clearLink() {
  localStorage.removeItem(LINK_KEY)
  clearLastThreadId()
}

export function loadLastThreadId(): string {
  return localStorage.getItem(LAST_KEY) ?? ""
}

export function saveLastThreadId(id: string) {
  const v = id.trim()
  if (!v) return
  localStorage.setItem(LAST_KEY, v)
}

export function clearLastThreadId() {
  localStorage.removeItem(LAST_KEY)
}
