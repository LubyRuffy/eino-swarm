import { act, renderHook } from "@testing-library/react"
import { StrictMode } from "react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { saveGatewaySession, type GatewaySession } from "./gateway-account"
import { useGatewayAccount } from "./use-gateway-account"
import { loadSavedLinks, saveLink } from "./store"

vi.mock("@capacitor/browser", () => ({ Browser: { open: vi.fn(async () => undefined) } }))
vi.mock("@capacitor/core", () => ({ Capacitor: { getPlatform: () => "ios", isNativePlatform: () => false }, registerPlugin: () => ({}) }))
const native = vi.hoisted(() => ({ resumed: undefined as ((state: { isActive: boolean }) => void) | undefined, removed: vi.fn() }))
vi.mock("@capacitor/app", () => ({ App: { addListener: vi.fn(async (_event: string, callback: (state: { isActive: boolean }) => void) => { native.resumed = callback; return { remove: async () => { native.removed() } } }) } }))

const origin = "https://ai.fofa.info:2440"
const session: GatewaySession = { origin, token: "agm_" + "b".repeat(64), user: { id: "u1", username: "user", approval_status: "approved" }, device: { id: "m1", expires_at: "2099-01-01T00:00:00Z" } }
const response = (data: unknown) => new Response(JSON.stringify({ data }))
const bootstrap = {
  user: session.user, device: session.device,
  models: { source_id: "gateway:abc", source_kind: "gateway", label: "服务器", execution: "phone", api_base_url: origin + "/v1", catalog_url: origin + "/v1/models", api_access_enabled: true },
  pairlink: { enabled: true, hub_url: origin, redeem_url: origin + "/pairlink/v1/pairings/redeem", websocket_url: origin.replace(/^http/, "ws") + "/pairlink/v1/ws", info_url: origin + "/pairlink/v1/info" },
  heartbeat_interval: 30, presence_ttl: 90,
}

beforeEach(() => { localStorage.clear(); vi.useFakeTimers(); vi.clearAllMocks(); native.resumed = undefined })
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

it("waits for review without PC/model requests, then activates the approved account without creating a binding", async () => {
  let polled = 0
  let candidate = ""
  const requested: string[] = []
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: RequestInit) => {
    requested.push(url)
    if (url.endsWith("/auth/mobile/start")) {
      candidate = (JSON.parse(String(init?.body)) as { candidate_token: string }).candidate_token
      return response({ device_code: "D".repeat(43), user_code: "abcd-1234-abcd-1234", verification_uri: origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=abcd-1234-abcd-1234", interval: 5, expires_at: new Date(Date.now() + 600_000).toISOString() })
    }
    if (url.endsWith("/auth/mobile/poll")) return response(++polled === 1 ? { status: "account_pending" } : { status: "authorized", mobile_id: "m1", user: session.user })
    expect((init?.headers as Record<string, string>).Authorization).toBe("Bearer " + candidate)
    if (url.endsWith("/auth/me")) return response({ user: session.user, device: session.device })
    if (url.endsWith("/bootstrap")) return response(bootstrap)
    if (url.includes("/link-hosts")) return response([])
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await result.current.login() })
  expect(result.current.status).toBe("authorization_pending")
  await act(async () => { await vi.advanceTimersByTimeAsync(2_000); native.resumed?.({ isActive: true }); await Promise.resolve() })
  expect(polled).toBe(0)
  await act(async () => { await vi.advanceTimersByTimeAsync(3_000) })
  expect(result.current.status).toBe("account_pending")
  expect(result.current.session).toBeNull()
  expect(requested.some((url) => /bootstrap|heartbeat|link-hosts|\/v1\/models/.test(url))).toBe(false)
  await act(async () => { await vi.advanceTimersByTimeAsync(5_000) })
  expect(result.current.status).toBe("authorized")
  expect(result.current.bootstrap?.models.source_id).toBe("gateway:abc")
  expect(result.current.session?.token).toBe(candidate)
  expect(requested.filter((url) => url.endsWith("/heartbeat"))).toHaveLength(1)
  expect(requested.some((url) => url.endsWith("/connect"))).toBe(false)
  expect(loadSavedLinks()).toEqual([])
  unmount()
  await Promise.resolve()
  expect(native.removed).toHaveBeenCalledTimes(1)
})

it("drops managed tickets on account downgrade and retains independent anonymous PCs", async () => {
  saveGatewaySession(session)
  saveLink({ hubURL: origin, hostPub: "owned", fingerprint: "owned", ticket: "t2", sessionID: "s2", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "pc-1" })
  saveLink({ hubURL: "https://anonymous.example.test", hostPub: "anon", fingerprint: "anon", ticket: "t1", sessionID: "s1" })
  const fetcher = vi.fn(async () => response({ user: { ...session.user, approval_status: "rejected" }, device: session.device }))
  vi.stubGlobal("fetch", fetcher)
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("account_rejected")
  expect(result.current.bootstrap).toBeUndefined()
  expect(result.current.hosts).toEqual([])
  expect(loadSavedLinks().map((saved) => saved.fingerprint)).toEqual(["anon"])
  expect(fetcher).toHaveBeenCalledTimes(1)
  unmount()
})

it("does not restore credentials when an old poll completes after signout", async () => {
  let release!: (value: Response) => void
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/mobile/start")) return response({ device_code: "D".repeat(43), user_code: "abcd-1234-abcd-1234", verification_uri: origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=abcd-1234-abcd-1234", interval: 5, expires_at: new Date(Date.now() + 600_000).toISOString() })
    return new Promise<Response>((resolve) => { release = resolve })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await result.current.login() })
  await act(async () => { await vi.advanceTimersByTimeAsync(5_000) })
  await act(async () => { await result.current.logout() })
  await act(async () => { release(response({ status: "account_pending" })); await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("signed_out")
  expect(result.current.session).toBeNull()
  unmount()
})

it("refreshes account and online presence on native foreground without waiting for the heartbeat timer", async () => {
  saveGatewaySession(session)
  let heartbeats = 0
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) return response({ user: session.user, device: session.device })
    if (url.endsWith("/bootstrap")) return response(bootstrap)
    if (url.includes("/link-hosts")) return response([])
    heartbeats++
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("authorized")
  expect(heartbeats).toBe(1)
  await act(async () => { native.resumed?.({ isActive: false }); await Promise.resolve() })
  expect(heartbeats).toBe(1)
  await act(async () => { native.resumed?.({ isActive: true }); await Promise.resolve(); await Promise.resolve() })
  expect(heartbeats).toBe(2)
  unmount()
})

it("a bootstrap authorization refusal closes managed resources and rechecks the actual review status", async () => {
  saveGatewaySession(session)
  saveLink({ hubURL: origin, hostPub: "owned", fingerprint: "owned", ticket: "t2", sessionID: "s2", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "pc-1", gatewayMobileID: "m1" })
  let reads = 0
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) return response({ user: { ...session.user, approval_status: ++reads === 1 ? "approved" : "rejected" }, device: session.device })
    if (url.endsWith("/bootstrap")) return new Response("{}", { status: 403 })
    throw new Error("no other resource request is authorized")
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("account_rejected")
  expect(result.current.hosts).toEqual([])
  expect(result.current.bootstrap).toBeUndefined()
  expect(loadSavedLinks()).toEqual([])
  expect(reads).toBe(2)
  unmount()
})

it("keeps an unverified bootstrap refusal restricted when its account recheck is unavailable", async () => {
  saveGatewaySession(session)
  saveLink({ hubURL: origin, hostPub: "owned", fingerprint: "owned", ticket: "t2", sessionID: "s2", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "pc-1", gatewayMobileID: "m1" })
  let reads = 0
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me") && ++reads === 1) return response({ user: session.user, device: session.device })
    return new Response("{}", { status: url.endsWith("/bootstrap") ? 403 : 503 })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("access_denied")
  expect(result.current.session?.user.approval_status).toBeUndefined()
  expect(result.current.bootstrap).toBeUndefined()
  expect(loadSavedLinks()).toEqual([])
  unmount()
})

it("does not restore a late connection across downgrade and reapproval of the same Mobile session", async () => {
  saveGatewaySession(session)
  let review = "approved"
  let release!: (value: Response) => void
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) return response({ user: { ...session.user, approval_status: review }, device: session.device })
    if (url.endsWith("/bootstrap")) return response(bootstrap)
    if (url.endsWith("/connect")) return new Promise<Response>((resolve) => { release = resolve })
    if (url.includes("/link-hosts")) return response([{ id: "pc-1", name: "PC", enabled: true, online: true }])
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  let connecting!: Promise<unknown>
  await act(async () => { connecting = result.current.connectHost("pc-1"); await Promise.resolve() })
  review = "rejected"
  await act(async () => { await result.current.refresh() })
  expect(result.current.status).toBe("account_rejected")
  review = "approved"
  await act(async () => { await result.current.refresh() })
  expect(result.current.status).toBe("authorized")
  const rejected = expect(connecting).rejects.toThrow("session_expired")
  await act(async () => { release(response({ binding_id: "b1", ticket: "c".repeat(64), host_pub: "AwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwM", session_id: "d".repeat(32) })); await rejected })
  expect(loadSavedLinks()).toEqual([])
  unmount()
})

it("restarts the initial account check after a StrictMode effect cleanup", async () => {
  saveGatewaySession(session)
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) return response({ user: session.user, device: session.device })
    if (url.endsWith("/bootstrap")) return response(bootstrap)
    if (url.includes("/link-hosts")) return response([])
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount, { wrapper: ({ children }) => <StrictMode>{children}</StrictMode> })
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("authorized")
  expect(result.current.bootstrap?.models.source_id).toBe("gateway:abc")
  unmount()
})

it.each([401, 403])("a current PC connection refusal %s immediately removes cached authority", async (status) => {
  saveGatewaySession(session)
  saveLink({ hubURL: origin, hostPub: "owned", fingerprint: "owned", ticket: "t2", sessionID: "s2", gatewayOrigin: origin, gatewayOwnerID: "u1", gatewayHostID: "pc-1", gatewayMobileID: "m1" })
  let rejected = false
  let reads = 0
  let release!: (value: Response) => void
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) { reads++; return response({ user: { ...session.user, approval_status: rejected ? "rejected" : "approved" }, device: session.device }) }
    if (url.endsWith("/bootstrap")) return response(bootstrap)
    if (url.endsWith("/connect")) return new Promise<Response>((resolve) => { release = resolve })
    if (url.includes("/link-hosts")) return response([{ id: "pc-1", name: "PC", enabled: true, online: true }])
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("authorized")
  let connecting!: Promise<unknown>
  await act(async () => { connecting = result.current.connectHost("pc-1"); await Promise.resolve() })
  rejected = true
  const refusal = expect(connecting).rejects.toThrow(status === 401 ? "session_expired" : "access_denied")
  await act(async () => { release(new Response("{}", { status })); await refusal })
  expect(result.current.status).toBe(status === 401 ? "expired" : "account_rejected")
  expect(result.current.hosts).toEqual([])
  expect(result.current.bootstrap).toBeUndefined()
  expect(loadSavedLinks()).toEqual([])
  if (status === 401) expect(result.current.session).toBeNull()
  expect(reads).toBe(status === 401 ? 1 : 2)
  unmount()
})

it("a stale PC refusal cannot revoke a newer Mobile login", async () => {
  saveGatewaySession(session)
  let current = session
  let release!: (value: Response) => void
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) return response({ user: current.user, device: current.device })
    if (url.endsWith("/bootstrap")) return response({ ...bootstrap, user: current.user, device: current.device })
    if (url.endsWith("/connect")) return new Promise<Response>((resolve) => { release = resolve })
    if (url.includes("/link-hosts")) return response([{ id: "pc-1", name: "PC", enabled: true, online: true }])
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  let connecting!: Promise<unknown>
  await act(async () => { connecting = result.current.connectHost("pc-1"); await Promise.resolve() })
  await act(async () => { await result.current.logout() })
  current = { ...session, token: "agm_" + "d".repeat(64), device: { ...session.device, id: "new-mobile" } }
  saveGatewaySession(current)
  await act(async () => { await result.current.refresh() })
  expect(result.current.status).toBe("authorized")
  const refusal = expect(connecting).rejects.toThrow("session_expired")
  await act(async () => { release(new Response("{}", { status: 401 })); await refusal })
  expect(result.current.status).toBe("authorized")
  expect(result.current.session?.device.id).toBe("new-mobile")
  expect(result.current.session?.token).toBe(current.token)
  expect(result.current.bootstrap?.device.id).toBe("new-mobile")
  unmount()
})

it.each([401, 403])("an old successful refresh cannot restore resources after a newer PC refusal %s", async (status) => {
  saveGatewaySession(session)
  let review = "approved"
  let holdHeartbeat = false
  let release!: (value: Response) => void
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) return response({ user: { ...session.user, approval_status: review }, device: session.device })
    if (url.endsWith("/bootstrap")) return response(bootstrap)
    if (url.endsWith("/connect")) { review = "rejected"; return new Response("{}", { status }) }
    if (url.includes("/link-hosts")) return response([{ id: "pc-1", name: "PC", enabled: true, online: true }])
    if (holdHeartbeat && url.endsWith("/heartbeat")) return new Promise<Response>((resolve) => { release = resolve })
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("authorized")
  holdHeartbeat = true
  let refreshing!: Promise<void>
  await act(async () => { refreshing = result.current.refresh(); await Promise.resolve(); await Promise.resolve() })
  expect(release).toBeTypeOf("function")
  await act(async () => { await expect(result.current.connectHost("pc-1")).rejects.toThrow(status === 401 ? "session_expired" : "access_denied") })
  expect(result.current.status).toBe(status === 401 ? "expired" : "account_rejected")
  await act(async () => { release(response({ ok: true })); await refreshing })
  expect(result.current.status).toBe(status === 401 ? "expired" : "account_rejected")
  expect(result.current.hosts).toEqual([])
  expect(result.current.bootstrap).toBeUndefined()
  expect(loadSavedLinks()).toEqual([])
  if (status === 401) expect(result.current.session).toBeNull()
  unmount()
})
