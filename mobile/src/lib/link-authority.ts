import type { SavedLink } from "./store"

/** The complete Hub base path matters: /team and / are separate authorities. */
export function hubEndpoint(raw: string): string | undefined {
  try {
    const url = new URL(raw)
    if (url.username || url.password || url.search || url.hash) return
    if (url.protocol === "wss:") url.protocol = "https:"
    if (url.protocol === "ws:") url.protocol = "http:"
    if (!["http:", "https:"].includes(url.protocol)) return
    return url.origin + url.pathname.replace(/\/+$/, "")
  } catch { return }
}

export function sameLinkAuthority(a: SavedLink | null | undefined, b: SavedLink | null | undefined): boolean {
  if (!a || !b) return a === b
  const endpoint = hubEndpoint(a.hubURL)
  return Boolean(endpoint && endpoint === hubEndpoint(b.hubURL)) &&
    a.fingerprint === b.fingerprint && a.hostPub === b.hostPub && a.ticket === b.ticket && a.sessionID === b.sessionID &&
    a.gatewayOrigin === b.gatewayOrigin && a.gatewayOwnerID === b.gatewayOwnerID &&
    a.gatewayHostID === b.gatewayHostID && a.gatewayMobileID === b.gatewayMobileID
}
