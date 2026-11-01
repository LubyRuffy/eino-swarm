import { b64urlToBytes, bytesToB64url } from "./bytes"
import { GATEWAY_ACCOUNT_EVENT, GatewayError, issueGatewayHostLink, loadGatewaySession, requireCurrentGatewaySession, type GatewayHost, type GatewaySession } from "./gateway-account"
import { hubEndpoint, sameLinkAuthority } from "./link-authority"
import { loadSavedLinks, replaceSavedLink, type SavedLink } from "./store"
import { parseOffer } from "./offer"

type Issuance = { scope: string; endpoint?: string; hostPub?: string; expected?: SavedLink; promise: Promise<SavedLink>; next?: SavedLink; settled: boolean }
// Shared by refreshes and the Connect button. A rotated result survives a failed
// local write, but only while its original saved pairing and account still exist.
const issuances = new Map<string, Issuance>()
const anonymousPairings = new Set<string>()
const scope = (session: GatewaySession) => JSON.stringify([session.origin, session.user.id, session.device.id, session.token])
const key = (session: GatewaySession, hostID: string) => scope(session) + ":" + hostID

export function sameGatewaySession(session: GatewaySession): boolean {
  const saved = loadGatewaySession()
  return Boolean(saved && scope(saved) === scope(session))
}

function current(session: GatewaySession): boolean {
  const saved = loadGatewaySession()
  return sameGatewaySession(session) && saved?.user.approval_status === "approved" && Date.parse(saved.device.expires_at) > Date.now()
}

function prune(session: GatewaySession) {
  const links = loadSavedLinks()
  for (const [id, entry] of issuances) {
    // An in-flight request can still rotate the server ticket. Keep its gate
    // even if the user forgets/re-scans the old record or changes accounts.
    if (entry.settled && (entry.scope !== scope(session) || (entry.expected && !links.some((saved) => sameLinkAuthority(saved, entry.expected))))) issuances.delete(id)
  }
}

function ownedAnonymous(session: GatewaySession, host: GatewayHost): SavedLink | undefined {
  if (!host.enabled || !host.online || host.owner_user_id !== session.user.id || !host.host_pub) return
  try { if (b64urlToBytes(host.host_pub).length !== 32) return } catch { return }
  const endpoint = hubEndpoint(session.origin)
  return loadSavedLinks().find((saved) => [saved.gatewayOwnerID, saved.gatewayOrigin, saved.gatewayMobileID, saved.gatewayHostID].every((value) => value === undefined) && hubEndpoint(saved.hubURL) === endpoint && saved.hostPub === host.host_pub)
}

/** The same issuance is shared with explicit selection until it is persisted. */
export async function accountHostLink(session: GatewaySession, host: GatewayHost, expected?: SavedLink): Promise<SavedLink> {
  requireCurrentGatewaySession(session)
  prune(session)
  const id = key(session, host.id)
  for (const [otherID, other] of issuances) if (otherID !== id && other.next && !other.expected) issuances.delete(otherID)
  let entry = issuances.get(id)
  if (!entry) {
    const original = expected || ownedAnonymous(session, host)
    const endpoint = hubEndpoint(session.origin)
    const hostPub = host.host_pub || original?.hostPub
    if (hostPub && (anonymousPairings.has(JSON.stringify([endpoint, hostPub])) || [...issuances.values()].some((other) => !other.settled && other.endpoint === endpoint && other.hostPub === hostPub))) throw new GatewayError(409, "pc_pairing_busy")
    entry = { scope: scope(session), endpoint, hostPub, expected: original, settled: false, promise: Promise.resolve(null as unknown as SavedLink) }
    const issuance = entry
    issuance.promise = issueGatewayHostLink(session, host.id).then((next) => {
      if (host.host_pub && next.hostPub !== host.host_pub) throw new GatewayError(0, "invalid_gateway_response")
      issuance.next = original ? { ...next, fingerprint: original.fingerprint } : next
      return issuance.next
    }).catch((error: unknown) => { if (issuances.get(id) === issuance) issuances.delete(id); throw error })
      .finally(() => { issuance.settled = true })
    issuances.set(id, issuance)
  }
  const saved = await entry.promise
  requireCurrentGatewaySession(session)
  return saved
}

export function completeAccountHostLink(session: GatewaySession, hostID: string, saved: SavedLink) {
  const id = key(session, hostID)
  if (sameLinkAuthority(issuances.get(id)?.next, saved)) issuances.delete(id)
}

window.addEventListener(GATEWAY_ACCOUNT_EVENT, () => {
  const session = loadGatewaySession()
  if (session) prune(session)
  else for (const [id, entry] of issuances) if (entry.settled) issuances.delete(id)
})

/** Never race QR redemption with an account ticket rotation for the same PC. */
export function beginAnonymousPairing(uri: string): () => void {
  const session = loadGatewaySession()
  if (session) prune(session)
  else for (const [id, entry] of issuances) if (entry.settled) issuances.delete(id)
  const offer = parseOffer(uri)
  const endpoint = hubEndpoint(offer.hubURL)
  const hostPub = bytesToB64url(offer.hostPub)
  const id = JSON.stringify([endpoint, hostPub])
  if (anonymousPairings.has(id) || [...issuances.values()].some((entry) => entry.endpoint === endpoint && entry.hostPub === hostPub)) throw new GatewayError(409, "pc_upgrade_pending")
  anonymousPairings.add(id)
  return () => { anonymousPairings.delete(id) }
}

/** A successful server rotation makes the old anonymous credential unusable. */
export function gatewayUpgradePending(saved: SavedLink): boolean {
  const session = loadGatewaySession()
  if (!session) return false
  return [...issuances.values()].some((entry) => entry.scope === scope(session) && entry.next && sameLinkAuthority(saved, entry.expected))
}

export async function upgradeAnonymousGatewayLinks(session: GatewaySession, hosts: GatewayHost[], canCommit: () => boolean): Promise<void> {
  prune(session)
  for (const host of hosts) {
    if (!canCommit() || !current(session)) return
    // Ambiguous duplicate identities must never choose an arbitrary host ID.
    if (hosts.filter((row) => row.host_pub === host.host_pub).length !== 1) continue
    const expected = ownedAnonymous(session, host)
    if (!expected) continue
    const next = await accountHostLink(session, host, expected)
    if (!canCommit() || !current(session)) return
    try {
      if (replaceSavedLink(expected, next)) window.dispatchEvent(new Event(GATEWAY_ACCOUNT_EVENT))
      completeAccountHostLink(session, host.id, next)
    } catch (error) {
      // Stop any retired anonymous transport. Retain the new ticket for a later
      // bounded refresh/save retry rather than issuing another rotation.
      window.dispatchEvent(new Event(GATEWAY_ACCOUNT_EVENT))
      throw error
    }
  }
}
