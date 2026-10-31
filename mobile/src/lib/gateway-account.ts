import { Browser } from "@capacitor/browser"
import { Capacitor } from "@capacitor/core"
import { sha256 } from "@noble/hashes/sha2.js"
import { softwareVersion } from "./app-update"
import { b64urlToBytes, bytesToB64url, bytesToHex, randomBytes, utf8 } from "./bytes"
import { fingerprint } from "./crypto"
import { SESSION_ID_SIZE } from "./frame"
import { hubDeviceFields } from "./device"
import { loadOrCreateIdentity, loadSavedLinks, removeLink, saveLink, type SavedLink } from "./store"

// Official builds have a working default; self-hosted installations can override it.
export const DEFAULT_GATEWAY_ORIGIN = "https://ai.fofa.info:2440"
export const GATEWAY_ACCOUNT_EVENT = "zwai.gateway.account"
const ORIGIN_KEY = "zwai.phone.gateway.origin"
const SESSION_KEY = "zwai.phone.gateway.session"
const PENDING_KEY = "zwai.phone.gateway.pending"
const CLIENT_PATH = "/client/api/v1"

export type GatewayStatus = "signed_out" | "checking" | "authorization_pending" | "account_pending" | "account_rejected" | "authorized" | "access_denied" | "expired" | "error"
export type GatewayUser = { id: string; username: string; approval_status?: string; enabled?: boolean }
export type GatewayDevice = { id: string; expires_at: string; name?: string; platform?: string }
export type GatewaySession = { origin: string; token: string; user: GatewayUser; device: GatewayDevice }
export type GatewayHost = { id: string; name: string; hostname: string; version: string; online: boolean; enabled: boolean; owner_user_id?: string }
export type GatewayBootstrap = {
  user: GatewayUser
  device: GatewayDevice
  models: { source_id: string; source_kind: "gateway"; label: string; execution: "phone"; api_base_url: string; catalog_url: string; api_access_enabled: boolean }
  pairlink: { enabled: boolean; hub_url: string; redeem_url: string; websocket_url: string; info_url: string }
  heartbeat_interval: number
  presence_ttl: number
}
export type GatewayPending = { origin: string; verifier: string; candidateToken: string; deviceCode: string; userCode: string; verificationURI: string; expiresAt: number; interval: number }
export type GatewayState = { status: GatewayStatus; session: GatewaySession; bootstrap?: GatewayBootstrap; hosts: GatewayHost[] }

export class GatewayError extends Error {
  constructor(readonly status: number, readonly code: string) {
    super(code)
    this.name = "GatewayError"
  }
}

export function normalizeGatewayOrigin(raw: string): string {
  let parsed: URL
  try { parsed = new URL(raw.trim()) } catch { throw new GatewayError(0, "invalid_gateway") }
  const local = parsed.hostname === "localhost" || parsed.hostname === "127.0.0.1" || parsed.hostname === "[::1]"
  if ((parsed.protocol !== "https:" && !(local && parsed.protocol === "http:")) || parsed.username || parsed.password || parsed.search || parsed.hash || parsed.pathname !== "/") {
    throw new GatewayError(0, "invalid_gateway")
  }
  return parsed.origin
}

export function getGatewayOrigin(): string {
  try { return normalizeGatewayOrigin(localStorage.getItem(ORIGIN_KEY) || DEFAULT_GATEWAY_ORIGIN) } catch { return DEFAULT_GATEWAY_ORIGIN }
}

export function setGatewayOrigin(origin: string) {
  const next = normalizeGatewayOrigin(origin)
  if (next !== getGatewayOrigin()) clearGatewaySession()
  localStorage.setItem(ORIGIN_KEY, next)
  localStorage.removeItem(PENDING_KEY)
  announce()
}

function announce() { window.dispatchEvent(new Event(GATEWAY_ACCOUNT_EVENT)) }

export function loadGatewaySession(): GatewaySession | null {
  try {
    const session = JSON.parse(localStorage.getItem(SESSION_KEY) || "null") as GatewaySession | null
    if (!session || normalizeGatewayOrigin(session.origin) !== session.origin || !/^agm_[a-f0-9]{64}$/.test(session.token) || !session.user.id || !session.device.id || !Number.isFinite(Date.parse(session.device.expires_at))) return null
    return session
  } catch { return null }
}

export function saveGatewaySession(session: GatewaySession) {
  localStorage.setItem(SESSION_KEY, JSON.stringify(session))
  localStorage.removeItem(PENDING_KEY)
  announce()
}

export function clearGatewaySession() {
  // Tickets carry account authority. An independent anonymous pairing remains independent.
  localStorage.removeItem(SESSION_KEY)
  localStorage.removeItem(PENDING_KEY)
  dropGatewayLinks()
}

export function dropGatewayLinks() {
  for (const link of loadSavedLinks()) if (link.gatewayOwnerID) removeLink(link.fingerprint)
  announce()
}

export function saveGatewayPending(pending: GatewayPending) {
  localStorage.setItem(PENDING_KEY, JSON.stringify(pending))
}

export function loadGatewayPending(): GatewayPending | null {
  try {
    const value = JSON.parse(localStorage.getItem(PENDING_KEY) || "null") as GatewayPending | null
    if (!value || value.origin !== getGatewayOrigin() || !Number.isFinite(value.expiresAt) || value.expiresAt <= Date.now() || !Number.isInteger(value.interval) || value.interval < 5 || value.interval > 60 || !/^agm_[a-f0-9]{64}$/.test(value.candidateToken) || !/^[A-Za-z0-9_-]{43}$/.test(value.verifier) || !/^[A-Za-z0-9_-]{43}$/.test(value.deviceCode)) return null
    validateVerification(value.origin, value.verificationURI, value.userCode)
    return value
  } catch { return null }
}

export function clearGatewayPending() { localStorage.removeItem(PENDING_KEY) }

async function request<T>(origin: string, path: string, fetcher: typeof fetch, token = "", body?: unknown, signal?: AbortSignal): Promise<{ data: T; meta?: { total?: number } }> {
  const headers: Record<string, string> = { "Content-Type": "application/json" }
  if (token) headers.Authorization = "Bearer " + token
  const controller = new AbortController()
  const aborted = () => controller.abort()
  signal?.addEventListener("abort", aborted, { once: true })
  if (signal?.aborted) controller.abort()
  const timer = window.setTimeout(() => controller.abort(), 15_000)
  try {
    const response = await fetcher(normalizeGatewayOrigin(origin) + CLIENT_PATH + path, {
      method: body === undefined ? "GET" : "POST", headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "omit", cache: "no-store", redirect: "error", signal: controller.signal,
    })
    if (!response.ok) throw new GatewayError(response.status, response.status === 401 ? "session_expired" : response.status === 403 ? "access_denied" : response.status === 409 ? "pc_unavailable" : response.status === 429 ? "rate_limited" : "gateway_unavailable")
    const text = await response.text()
    if (text.length > (2 << 20)) throw new GatewayError(0, "invalid_gateway_response")
    const value = JSON.parse(text) as { data: T; meta?: { total?: number } }
    if (!value || typeof value !== "object" || !("data" in value)) throw new GatewayError(0, "invalid_gateway_response")
    return value
  } catch (err) {
    if (controller.signal.aborted && !signal?.aborted) throw new GatewayError(0, "gateway_timeout")
    throw err
  } finally {
    window.clearTimeout(timer)
    signal?.removeEventListener("abort", aborted)
  }
}

function validateVerification(origin: string, raw: string, publicCode: string) {
  const url = new URL(raw)
  if (url.origin !== origin || url.username || url.password || url.hash || url.pathname !== CLIENT_PATH + "/auth/sso/nosec/start" || [...url.searchParams.keys()].length !== 1 || url.searchParams.get("mobile_code") !== publicCode || !/^[a-fA-F0-9]{4}(?:-[a-fA-F0-9]{4}){3}$/.test(publicCode)) {
    throw new GatewayError(0, "invalid_verification_url")
  }
}

function mobilePlatform(): "ios" | "android" {
  const platform = Capacitor.getPlatform()
  if (platform === "ios" || platform === "android") return platform
  return /iPhone|iPad|iPod/i.test(navigator.userAgent) ? "ios" : "android"
}

export async function startGatewayLogin(origin = getGatewayOrigin(), fetcher: typeof fetch = fetch, signal?: AbortSignal): Promise<GatewayPending> {
  const base = normalizeGatewayOrigin(origin)
  const verifier = bytesToB64url(randomBytes(32))
  const candidateToken = "agm_" + bytesToHex(randomBytes(32))
  const version = await softwareVersion()
  const { name } = hubDeviceFields(version)
  const { data } = await request<{ device_code: string; user_code: string; verification_uri: string; expires_at: string; interval: number }>(base, "/auth/mobile/start", fetcher, "", {
    name, version, device_pub: bytesToB64url(loadOrCreateIdentity().pub), platform: mobilePlatform(),
    candidate_token: candidateToken, code_challenge: bytesToB64url(sha256(utf8(verifier))),
  }, signal)
  validateVerification(base, data.verification_uri, data.user_code)
  const expiresAt = Date.parse(data.expires_at)
  if (!/^[A-Za-z0-9_-]{43}$/.test(data.device_code) || !Number.isFinite(expiresAt) || expiresAt <= Date.now() || expiresAt > Date.now() + 610_000 || !Number.isInteger(data.interval) || data.interval < 5 || data.interval > 60) throw new GatewayError(0, "invalid_gateway_response")
  const pending = { origin: base, verifier, candidateToken, deviceCode: data.device_code, userCode: data.user_code, verificationURI: data.verification_uri, expiresAt, interval: data.interval }
  if (signal?.aborted) throw new DOMException("Aborted", "AbortError")
  saveGatewayPending(pending)
  await Browser.open({ url: pending.verificationURI })
  return pending
}

export async function pollGatewayLogin(pending: GatewayPending, fetcher: typeof fetch = fetch, signal?: AbortSignal): Promise<{ status: GatewayStatus; session?: GatewaySession }> {
  if (Date.now() >= pending.expiresAt) return { status: "expired" }
  const { data } = await request<{ status: GatewayStatus; user?: GatewayUser; mobile_id?: string }>(pending.origin, "/auth/mobile/poll", fetcher, "", { device_code: pending.deviceCode, code_verifier: pending.verifier }, signal)
  if (!["authorization_pending", "account_pending", "account_rejected", "authorized", "access_denied", "expired"].includes(data.status)) throw new GatewayError(0, "invalid_gateway_response")
  if (data.status !== "authorized") return { status: data.status }
  const { data: principal } = await request<{ user: GatewayUser; device: GatewayDevice }>(pending.origin, "/auth/me", fetcher, pending.candidateToken, undefined, signal)
  if (!principal.user?.id || principal.user.id !== data.user?.id || !principal.device?.id || principal.device.id !== data.mobile_id || !Number.isFinite(Date.parse(principal.device.expires_at))) throw new GatewayError(0, "invalid_gateway_response")
  return { status: "authorized", session: { origin: pending.origin, token: pending.candidateToken, ...principal } }
}

export async function readGatewaySession(session: GatewaySession, fetcher: typeof fetch = fetch, signal?: AbortSignal): Promise<GatewaySession> {
  if (Date.parse(session.device.expires_at) <= Date.now()) throw new GatewayError(401, "session_expired")
  const { data: principal } = await request<{ user: GatewayUser; device: GatewayDevice }>(session.origin, "/auth/me", fetcher, session.token, undefined, signal)
  if (principal.user?.id !== session.user.id || principal.device?.id !== session.device.id || principal.user.enabled === false || !Number.isFinite(Date.parse(principal.device.expires_at)) || Date.parse(principal.device.expires_at) <= Date.now()) throw new GatewayError(401, "session_expired")
  return { ...session, ...principal }
}

export async function refreshGatewaySession(session: GatewaySession, fetcher: typeof fetch = fetch, signal?: AbortSignal): Promise<GatewayState> {
  const current = await readGatewaySession(session, fetcher, signal)
  if (current.user.approval_status !== "approved") return { status: current.user.approval_status === "rejected" ? "account_rejected" : "account_pending", session: current, hosts: [] }
  const { data: bootstrap } = await request<GatewayBootstrap>(session.origin, "/bootstrap", fetcher, session.token, undefined, signal)
  validateBootstrap(session.origin, bootstrap, current)
  const hosts: GatewayHost[] = []
  if (bootstrap.pairlink.enabled) {
    for (let page = 1; page <= 100; page++) {
      const { data, meta } = await request<GatewayHost[]>(session.origin, `/link-hosts?page=${page}&page_size=100`, fetcher, session.token, undefined, signal)
      if (!Array.isArray(data)) throw new GatewayError(0, "invalid_gateway_response")
      hosts.push(...data)
      if (data.length < 100 || hosts.length >= (meta?.total ?? Infinity)) break
    }
  }
  await request(session.origin, "/heartbeat", fetcher, session.token, {}, signal)
  return { status: "authorized", session: current, bootstrap, hosts }
}

function validateBootstrap(origin: string, bootstrap: GatewayBootstrap, session: GatewaySession) {
  if (bootstrap.user?.id !== session.user.id || bootstrap.device?.id !== session.device.id || !bootstrap.models?.source_id?.startsWith("gateway:") || !bootstrap.pairlink) throw new GatewayError(0, "invalid_gateway_response")
  const expected = [
    [bootstrap.models.api_base_url, origin + "/v1"], [bootstrap.models.catalog_url, origin + "/v1/models"],
    [bootstrap.pairlink.hub_url, origin], [bootstrap.pairlink.redeem_url, origin + "/pairlink/v1/pairings/redeem"],
    [bootstrap.pairlink.info_url, origin + "/pairlink/v1/info"], [bootstrap.pairlink.websocket_url, origin.replace(/^http/, "ws") + "/pairlink/v1/ws"],
  ]
  if (expected.some(([value, target]) => value !== target)) throw new GatewayError(0, "invalid_gateway_response")
}

export async function logoutGatewaySession(session: GatewaySession, fetcher: typeof fetch = fetch) {
  await request(session.origin, "/auth/logout", fetcher, session.token, {})
}

export async function issueGatewayHostLink(session: GatewaySession, hostID: string, fetcher: typeof fetch = fetch): Promise<SavedLink> {
  requireCurrentGatewaySession(session)
  if (!hostID) throw new GatewayError(400, "invalid_pc")
  const { data } = await request<{ binding_id: string; ticket: string; host_pub: string; session_id: string }>(session.origin, "/link-hosts/" + encodeURIComponent(hostID) + "/connect", fetcher, session.token, {
    device_pub: bytesToB64url(loadOrCreateIdentity().pub), ...hubDeviceFields(await softwareVersion()),
  })
  requireCurrentGatewaySession(session)
  const hostPub = b64urlToBytes(data.host_pub)
  if (hostPub.length !== 32 || !/^[a-f0-9]+$/i.test(data.session_id) || data.session_id.length !== SESSION_ID_SIZE * 2 || !/^[a-f0-9]{64}$/i.test(data.ticket) || !data.binding_id) throw new GatewayError(0, "invalid_gateway_response")
  return { hubURL: session.origin, ticket: data.ticket, hostPub: data.host_pub, sessionID: data.session_id, fingerprint: fingerprint(hostPub), gatewayOrigin: session.origin, gatewayOwnerID: session.user.id, gatewayHostID: hostID, gatewayMobileID: session.device.id }
}

function requireCurrentGatewaySession(session: GatewaySession) {
  const current = loadGatewaySession()
  if (!current || current.token !== session.token || current.origin !== session.origin || current.user.id !== session.user.id || current.device.id !== session.device.id || Date.parse(current.device.expires_at) <= Date.now()) throw new GatewayError(401, "session_expired")
  if (current.user.approval_status !== "approved") throw new GatewayError(403, "access_denied")
}

export async function connectGatewayHost(session: GatewaySession, hostID: string, fetcher: typeof fetch = fetch): Promise<SavedLink> {
  const saved = await issueGatewayHostLink(session, hostID, fetcher)
  saveLink(saved)
  return saved
}

export function gatewayLinkForReconnect(saved: SavedLink): SavedLink {
  const session = loadGatewaySession()
  if (!session || session.origin !== saved.gatewayOrigin || session.user.id !== saved.gatewayOwnerID || session.device.id !== saved.gatewayMobileID || Date.parse(session.device.expires_at) <= Date.now() || !saved.gatewayHostID) throw new GatewayError(401, "session_expired")
  if (session.user.approval_status !== "approved") throw new GatewayError(403, "access_denied")
  const current = loadSavedLinks().find((row) => row.fingerprint === saved.fingerprint && row.hostPub === saved.hostPub && row.gatewayOwnerID === saved.gatewayOwnerID && row.gatewayOrigin === saved.gatewayOrigin && row.gatewayHostID === saved.gatewayHostID && row.gatewayMobileID === saved.gatewayMobileID)
  if (!current) throw new GatewayError(403, "pc_unlinked")
  // The Hub validates the ticket's live authority. Reconnect cannot undo a
  // revoked binding or spend another binding row by issuing a replacement.
  return current
}
