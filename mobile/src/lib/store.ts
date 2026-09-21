import { b64urlToBytes, bytesToB64url } from "./bytes"
import { fromPrivate, generateIdentity, type Identity } from "./crypto"

const ID_KEY = "zwai.remote.identity"
const LINK_KEY = "zwai.remote.link"
const LINKS_KEY = "zwai.remote.links"
const ACTIVE_KEY = "zwai.remote.active"
const LAST_KEY = "zwai.remote.lastThread"

export type SavedLink = {
  hubURL: string
  ticket: string
  hostPub: string
  sessionID: string
  fingerprint: string
  label?: string
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

function asLink(v: unknown): SavedLink | null {
  if (!v || typeof v !== "object") return null
  const row = v as Partial<SavedLink>
  if (!row.hubURL || !row.ticket || !row.hostPub || !row.sessionID) return null
  return {
    hubURL: row.hubURL,
    ticket: row.ticket,
    hostPub: row.hostPub,
    sessionID: row.sessionID,
    fingerprint: row.fingerprint || row.hostPub,
    label: row.label,
  }
}

function writeLinks(links: SavedLink[]) {
  localStorage.setItem(LINKS_KEY, JSON.stringify(links))
  if (links.length === 0) {
    localStorage.removeItem(ACTIVE_KEY)
    localStorage.removeItem(LINKS_KEY)
    localStorage.removeItem(LINK_KEY)
    return
  }
  const active = loadActiveFingerprint()
  if (!links.some((row) => row.fingerprint === active)) {
    localStorage.setItem(ACTIVE_KEY, links[0].fingerprint)
  }
}

export function loadSavedLinks(): SavedLink[] {
  const raw = localStorage.getItem(LINKS_KEY)
  if (raw) {
    try {
      const rows = JSON.parse(raw) as unknown
      if (Array.isArray(rows)) {
        const links = rows.map(asLink).filter((row): row is SavedLink => Boolean(row))
        if (links.length) return links
      }
    } catch {
      // migrate below
    }
  }
  const legacyRaw = localStorage.getItem(LINK_KEY)
  if (!legacyRaw) return []
  try {
    const one = asLink(JSON.parse(legacyRaw) as unknown)
    if (!one) return []
    writeLinks([one])
    localStorage.removeItem(LINK_KEY)
    localStorage.setItem(ACTIVE_KEY, one.fingerprint)
    return [one]
  } catch {
    return []
  }
}

export function loadSavedLink(): SavedLink | null {
  const links = loadSavedLinks()
  if (links.length === 0) return null
  const fp = loadActiveFingerprint()
  return links.find((row) => row.fingerprint === fp) ?? links[0]
}

export function loadActiveFingerprint(): string {
  return localStorage.getItem(ACTIVE_KEY) ?? ""
}

export function saveActiveFingerprint(fp: string) {
  const v = fp.trim()
  if (!v) return
  if (!loadSavedLinks().some((row) => row.fingerprint === v)) return
  localStorage.setItem(ACTIVE_KEY, v)
}

export function saveLink(link: SavedLink) {
  const next = asLink(link)
  if (!next) return
  const links = loadSavedLinks()
  const previous = links.find((row) => row.fingerprint === next.fingerprint)
  if (previous?.label?.trim() && !next.label?.trim()) {
    next.label = previous.label
  }
  const rest = links.filter((row) => row.fingerprint !== next.fingerprint)
  rest.push(next)
  localStorage.setItem(ACTIVE_KEY, next.fingerprint)
  const legacyLast = localStorage.getItem(LAST_KEY)
  if (legacyLast && !localStorage.getItem(LAST_KEY + "." + next.fingerprint)) {
    localStorage.setItem(LAST_KEY + "." + next.fingerprint, legacyLast)
  }
  writeLinks(rest)
}

/** Cache the name this PC sent on hello/list. Does not steal the active chip. */
export function saveHostLabel(fingerprint: string, name: string) {
  const v = name.trim()
  if (!v || !fingerprint) return
  const links = loadSavedLinks()
  let changed = false
  const next = links.map((row) => {
    if (row.fingerprint !== fingerprint || row.label === v) return row
    changed = true
    return { ...row, label: v }
  })
  if (changed) writeLinks(next)
}

export function removeLink(fingerprint: string): SavedLink[] {
  const links = loadSavedLinks().filter((row) => row.fingerprint !== fingerprint)
  clearLastThreadId(fingerprint)
  writeLinks(links)
  return links
}

export function clearLink() {
  for (const row of loadSavedLinks()) clearLastThreadId(row.fingerprint)
  localStorage.removeItem(LINKS_KEY)
  localStorage.removeItem(ACTIVE_KEY)
  localStorage.removeItem(LINK_KEY)
  localStorage.removeItem(LAST_KEY)
}

export function loadLastThreadId(): string {
  const fp = loadActiveFingerprint()
  if (fp) {
    const v = localStorage.getItem(LAST_KEY + "." + fp)
    if (v) return v
  }
  return localStorage.getItem(LAST_KEY) ?? ""
}

export function saveLastThreadId(id: string) {
  const v = id.trim()
  if (!v) return
  const fp = loadActiveFingerprint()
  localStorage.setItem(fp ? LAST_KEY + "." + fp : LAST_KEY, v)
}

export function clearLastThreadId(fingerprint?: string) {
  const fp = fingerprint || loadActiveFingerprint()
  if (fp) localStorage.removeItem(LAST_KEY + "." + fp)
  if (!fingerprint) localStorage.removeItem(LAST_KEY)
}

export function hubHost(hubURL: string): string {
  try {
    return new URL(hubURL.replace(/^ws/i, "http")).hostname
  } catch {
    return ""
  }
}

/** Chip text: the name this PC sent, else a short fingerprint. Never the hub. */
export function linkLabel(link: SavedLink, all: SavedLink[] = []): string {
  const named = link.label?.trim()
  if (named) {
    const clashes = all.filter(
      (row) => row.fingerprint !== link.fingerprint && row.label?.trim() === named,
    )
    if (clashes.length) return named + " · " + link.fingerprint.slice(0, 4)
    return named
  }
  return (link.fingerprint || "").slice(0, 8) || "?"
}
