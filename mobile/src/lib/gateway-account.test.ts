import { beforeEach, describe, expect, it, vi } from "vitest"
import { bytesToB64url } from "./bytes"
import { loadActiveFingerprint, loadOrCreateIdentity, loadSavedLinks, removeLink, saveLink } from "./store"
import {
  clearGatewaySession, connectGatewayHost, getGatewayOrigin, loadGatewaySession,
  gatewayLinkForReconnect, logoutGatewaySession, pollGatewayLogin, refreshGatewaySession, saveGatewaySession, setGatewayOrigin,
  startGatewayLogin, type GatewaySession,
} from "./gateway-account"

vi.mock("@capacitor/browser", () => ({ Browser: { open: vi.fn(async () => undefined) } }))
vi.mock("@capacitor/core", () => ({ Capacitor: { getPlatform: () => "ios", isNativePlatform: () => false }, registerPlugin: () => ({}) }))

const origin = "https://gateway.example.test"
const session: GatewaySession = { origin, token: "agm_" + "a".repeat(64), user: { id: "u1", username: "account", approval_status: "approved" }, device: { id: "m1", expires_at: "2099-01-01T00:00:00Z" } }
const ok = (data: unknown, status = 200) => new Response(JSON.stringify({ data }), { status })

beforeEach(() => { localStorage.clear(); vi.clearAllMocks() })

describe("Gateway account authority", () => {
  it("needs no manual server configuration and validates custom origins", () => {
    expect(getGatewayOrigin()).toBe("https://ai.fofa.info:2440")
    setGatewayOrigin(origin + "/")
    expect(getGatewayOrigin()).toBe(origin)
    for (const invalid of ["https://user:secret@example.test", "http://remote.example.test", origin + "/api", origin + "?token=secret"]) {
      expect(() => setGatewayOrigin(invalid)).toThrow()
    }
  })

  it("stages S256 proof without credentials in the browser URL and polls without a candidate token", async () => {
    const code = "abcd-1234-abcd-1234"
    let submitted: Record<string, string> = {}
    const fetcher = vi.fn(async (input: string | URL | Request, init?: RequestInit) => {
      expect(String(input)).toBe(origin + "/client/api/v1/auth/mobile/start")
      expect(init?.credentials).toBe("omit")
      expect(init?.redirect).toBe("error")
      submitted = JSON.parse(String(init?.body))
      expect(submitted.device_pub).toBe(bytesToB64url(loadOrCreateIdentity().pub))
      expect(submitted.candidate_token).toMatch(/^agm_[a-f0-9]{64}$/)
      expect(submitted.code_challenge).toMatch(/^[A-Za-z0-9_-]{43}$/)
      expect(submitted.platform).toBe("ios")
      return ok({ device_code: "D".repeat(43), user_code: code, interval: 5, expires_at: new Date(Date.now() + 600_000).toISOString(), verification_uri: origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=" + code })
    }) as typeof fetch
    const pending = await startGatewayLogin(origin, fetcher)
    expect(pending.verifier).toMatch(/^[A-Za-z0-9_-]{43}$/)
    expect(pending.candidateToken).toBe(submitted.candidate_token)
    expect(pending.verificationURI).not.toContain(pending.verifier)
    expect(pending.verificationURI).not.toContain(pending.candidateToken)
    expect(loadGatewaySession()).toBeNull()
    const poller = vi.fn(async (_url, init) => {
      expect(JSON.parse(String(init?.body))).toEqual({ device_code: pending.deviceCode, code_verifier: pending.verifier })
      expect(init?.headers).not.toHaveProperty("Authorization")
      return ok({ status: "account_pending" })
    }) as typeof fetch
    expect((await pollGatewayLogin(pending, poller)).status).toBe("account_pending")
    expect(loadGatewaySession()).toBeNull()
  })

  it("refuses a forged verification URL before opening a browser", async () => {
    const { Browser } = await import("@capacitor/browser")
    for (const verification_uri of ["https://foreign.example.test/", origin + "/untrusted", origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=wrong"]) {
      const fetcher = vi.fn(async () => ok({ device_code: "D".repeat(43), user_code: "abcd-1234-abcd-1234", interval: 5, expires_at: new Date(Date.now() + 600_000).toISOString(), verification_uri })) as typeof fetch
      await expect(startGatewayLogin(origin, fetcher)).rejects.toThrow()
    }
    expect(Browser.open).not.toHaveBeenCalled()
  })

  it("does not fetch bootstrap, PC lists or heartbeat for a downgraded account", async () => {
    const fetcher = vi.fn(async () => ok({ user: { ...session.user, approval_status: "pending" }, device: session.device })) as typeof fetch
    const state = await refreshGatewaySession(session, fetcher)
    expect(state.status).toBe("account_pending")
    expect(state.bootstrap).toBeUndefined()
    expect(state.hosts).toEqual([])
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it("selects exactly one PC without QR and keeps account ownership with the saved link", async () => {
    saveGatewaySession(session)
    const pub = new Uint8Array(32).fill(3)
    const fetcher = vi.fn(async (input, init) => {
      expect(input).toBe(origin + "/client/api/v1/link-hosts/pc-1/connect")
      expect(init?.headers).toEqual({ "Content-Type": "application/json", Authorization: "Bearer " + session.token })
      const body = JSON.parse(String(init?.body))
      expect(body.device_pub).toBe(bytesToB64url(loadOrCreateIdentity().pub))
      expect(body).not.toHaveProperty("code")
      return ok({ binding_id: "b1", ticket: "c".repeat(64), host_pub: bytesToB64url(pub), session_id: "d".repeat(32) })
    }) as typeof fetch
    const saved = await connectGatewayHost(session, "pc-1", fetcher)
    expect(saved).toMatchObject({ gatewayOwnerID: "u1", gatewayHostID: "pc-1", gatewayOrigin: origin, sessionID: "d".repeat(32) })
    expect(loadSavedLinks()).toEqual([saved])
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it("signout removes managed tickets while preserving independent anonymous pairings", () => {
    saveGatewaySession(session)
    saveLink({ hubURL: origin, hostPub: "anon", fingerprint: "anon", ticket: "t1", sessionID: "s1" })
    saveLink({ hubURL: origin, hostPub: "owned", fingerprint: "owned", ticket: "t2", sessionID: "s2", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "pc-1" })
    clearGatewaySession()
    expect(loadGatewaySession()).toBeNull()
    expect(loadSavedLinks().map((link) => link.fingerprint)).toEqual(["anon"])
  })

  it("does not install a late PC connection after signout", async () => {
    saveGatewaySession(session)
    let release!: (value: Response) => void
    const fetcher = vi.fn(() => new Promise<Response>((resolve) => { release = resolve })) as typeof fetch
    const connecting = connectGatewayHost(session, "pc-1", fetcher)
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
    clearGatewaySession()
    release(ok({ binding_id: "b1", ticket: "c".repeat(64), host_pub: bytesToB64url(new Uint8Array(32).fill(3)), session_id: "d".repeat(32) }))
    await expect(connecting).rejects.toThrow("session_expired")
    expect(loadSavedLinks()).toEqual([])
  })

  it("checks current account authority before fetching approved resources and never sends cookies", async () => {
    const seen: string[] = []
    const bootstrap = {
      user: { ...session.user, approval_status: "approved" }, device: session.device,
      models: { source_id: "gateway:source", source_kind: "gateway", label: "服务器", execution: "phone", api_base_url: origin + "/v1", catalog_url: origin + "/v1/models", api_access_enabled: false },
      pairlink: { enabled: true, hub_url: origin, redeem_url: origin + "/pairlink/v1/pairings/redeem", websocket_url: "wss://gateway.example.test/pairlink/v1/ws", info_url: origin + "/pairlink/v1/info" },
      heartbeat_interval: 30, presence_ttl: 90,
    }
    const fetcher = vi.fn(async (input, init) => {
      const url = String(input)
      seen.push(url)
      expect(init?.credentials).toBe("omit")
      expect(init?.headers).toEqual({ "Content-Type": "application/json", Authorization: "Bearer " + session.token })
      if (url.endsWith("/auth/me")) return ok({ user: bootstrap.user, device: session.device })
      if (url.endsWith("/bootstrap")) return ok(bootstrap)
      if (url.includes("/link-hosts")) return new Response(JSON.stringify({ data: [{ id: "pc-1", name: "workstation", enabled: true, online: true }], meta: { total: 1 } }))
      return ok({ ok: true })
    }) as typeof fetch
    const state = await refreshGatewaySession(session, fetcher)
    expect(state.status).toBe("authorized")
    expect(state.hosts.map((host) => host.id)).toEqual(["pc-1"])
    expect(state.bootstrap?.models.api_access_enabled).toBe(false)
    expect(seen).toEqual([origin + "/client/api/v1/auth/me", origin + "/client/api/v1/bootstrap", origin + "/client/api/v1/link-hosts?page=1&page_size=100", origin + "/client/api/v1/heartbeat"])
    bootstrap.models.api_base_url = "https://foreign.example.test/v1"
    seen.length = 0
    await expect(refreshGatewaySession(session, fetcher)).rejects.toThrow("invalid_gateway_response")
    expect(seen).toHaveLength(2)
  })

  it("does not accept another Mobile identity or authenticate a closed session", async () => {
    const foreign = vi.fn(async () => ok({ user: { ...session.user, id: "other" }, device: session.device })) as typeof fetch
    await expect(refreshGatewaySession(session, foreign)).rejects.toThrow("session_expired")
    const expired = { ...session, device: { ...session.device, expires_at: "2000-01-01T00:00:00Z" } }
    const fetcher = vi.fn() as unknown as typeof fetch
    await expect(refreshGatewaySession(expired, fetcher)).rejects.toThrow("session_expired")
    expect(fetcher).not.toHaveBeenCalled()
  })

  it("rejects an invalid Pairlink session size before persisting a ticket", async () => {
    saveGatewaySession(session)
    const fetcher = vi.fn(async () => ok({ binding_id: "b1", ticket: "c".repeat(64), host_pub: bytesToB64url(new Uint8Array(32).fill(3)), session_id: "d".repeat(64) })) as typeof fetch
    await expect(connectGatewayHost(session, "pc-1", fetcher)).rejects.toThrow("invalid_gateway_response")
    expect(loadSavedLinks()).toEqual([])
  })

  it("reconnect reuses the existing ticket without stealing a newer selection or reviving an unlinked PC", () => {
    saveGatewaySession(session)
    const hostPub = bytesToB64url(new Uint8Array(32).fill(3))
    const saved = { hubURL: origin, ticket: "c".repeat(64), hostPub, sessionID: "d".repeat(32), fingerprint: "a", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "pc-a", gatewayMobileID: "m1" }
    saveLink(saved)
    saveLink({ hubURL: origin, ticket: "other-ticket", hostPub: "other-pub", sessionID: "other-session", fingerprint: "b" })
    for (let count = 0; count < 5; count++) expect(gatewayLinkForReconnect(saved)).toEqual(saved)
    expect(loadActiveFingerprint()).toBe("b")
    expect(loadSavedLinks().find((row) => row.fingerprint === saved.fingerprint)?.ticket).toBe("c".repeat(64))
    removeLink(saved.fingerprint)
    expect(() => gatewayLinkForReconnect(saved)).toThrow("pc_unlinked")
    expect(loadSavedLinks().map((row) => row.fingerprint)).toEqual(["b"])
  })

  it("does not reuse a ticket from a different Mobile login session", () => {
    saveGatewaySession(session)
    const saved = { hubURL: origin, ticket: "c".repeat(64), hostPub: bytesToB64url(new Uint8Array(32).fill(3)), sessionID: "d".repeat(32), fingerprint: "a", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "pc-a", gatewayMobileID: "older-mobile" }
    saveLink(saved)
    expect(() => gatewayLinkForReconnect(saved)).toThrow("session_expired")
  })

  it("a delayed old logout cannot erase a newer account session", async () => {
    saveGatewaySession(session)
    let release!: (value: Response) => void
    const fetcher = vi.fn(() => new Promise<Response>((resolve) => { release = resolve })) as typeof fetch
    const loggingOut = logoutGatewaySession(session, fetcher)
    clearGatewaySession()
    const next = { ...session, token: "agm_" + "b".repeat(64), device: { ...session.device, id: "m2" } }
    saveGatewaySession(next)
    release(ok({ ok: true }))
    await loggingOut
    expect(loadGatewaySession()).toEqual(next)
  })

  it.each(["pending", "rejected", "expired"])("does not install a late connection after account becomes %s", async (state) => {
    saveGatewaySession(session)
    let release!: (value: Response) => void
    const fetcher = vi.fn(() => new Promise<Response>((resolve) => { release = resolve })) as typeof fetch
    const connecting = connectGatewayHost(session, "pc-1", fetcher)
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1))
    saveGatewaySession(state === "expired" ? { ...session, device: { ...session.device, expires_at: "2000-01-01T00:00:00Z" } } : { ...session, user: { ...session.user, approval_status: state } })
    release(ok({ binding_id: "b1", ticket: "c".repeat(64), host_pub: bytesToB64url(new Uint8Array(32).fill(3)), session_id: "d".repeat(32) }))
    await expect(connecting).rejects.toThrow(state === "expired" ? "session_expired" : "access_denied")
    expect(loadSavedLinks()).toEqual([])
  })
})
