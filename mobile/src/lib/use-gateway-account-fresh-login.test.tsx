import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { saveGatewaySession, type GatewaySession } from "./gateway-account"
import { useGatewayAccount } from "./use-gateway-account"

vi.mock("@capacitor/browser", () => ({ Browser: { open: vi.fn(async () => undefined) } }))
vi.mock("@capacitor/core", () => ({ Capacitor: { getPlatform: () => "ios", isNativePlatform: () => false }, registerPlugin: () => ({}) }))
const native = vi.hoisted(() => ({ resumed: undefined as ((state: { isActive: boolean }) => void) | undefined }))
vi.mock("@capacitor/app", () => ({ App: { addListener: vi.fn(async (_event: string, callback: (state: { isActive: boolean }) => void) => { native.resumed = callback; return { remove: async () => undefined } }) } }))

const origin = "https://ai.fofa.info:2440"
const session: GatewaySession = { origin, token: "agm_" + "c".repeat(64), user: { id: "u1", username: "user", approval_status: "approved" }, device: { id: "m1", expires_at: "2099-01-01T00:00:00Z" } }
const response = (data: unknown) => new Response(JSON.stringify({ data }))
const bootstrap = {
  user: session.user, device: session.device,
  models: { source_id: "gateway:abc", source_kind: "gateway", label: "Server", execution: "phone", api_base_url: origin + "/v1", catalog_url: origin + "/v1/models", api_access_enabled: true },
  pairlink: { enabled: true, hub_url: origin, redeem_url: origin + "/pairlink/v1/pairings/redeem", websocket_url: origin.replace(/^http/, "ws") + "/pairlink/v1/ws", info_url: origin + "/pairlink/v1/info" },
  heartbeat_interval: 30, presence_ttl: 90,
}

// A sign-in whose first poll already authorizes, so the only variable is what
// the hook says about it afterwards.
function stubGateway() {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/mobile/start")) {
      return response({ device_code: "D".repeat(43), user_code: "abcd-1234-abcd-1234", verification_uri: origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=abcd-1234-abcd-1234", interval: 5, expires_at: new Date(Date.now() + 600_000).toISOString() })
    }
    if (url.endsWith("/auth/mobile/poll")) return response({ status: "authorized", mobile_id: "m1", user: session.user })
    if (url.endsWith("/auth/me")) return response({ user: session.user, device: session.device })
    if (url.endsWith("/bootstrap")) return response(bootstrap)
    if (url.includes("/link-hosts")) return response([{ id: "pc-1", name: "Desktop", hostname: "pc", enabled: true, online: true, owner_user_id: "u1" }])
    return response({ ok: true })
  }))
}

beforeEach(() => { localStorage.clear(); vi.useFakeTimers(); vi.clearAllMocks(); native.resumed = undefined })
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals() })

it("marks a completed sign-in as fresh only once its first account read has loaded the computers", async () => {
  stubGateway()
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await result.current.login() })
  expect(result.current.freshLogin).toBe(false)
  await act(async () => { await vi.advanceTimersByTimeAsync(5_000) })
  expect(result.current.status).toBe("authorized")
  expect(result.current.hosts.map((host) => host.id)).toEqual(["pc-1"])
  expect(result.current.freshLogin).toBe(true)
  act(() => result.current.consumeFreshLogin())
  expect(result.current.freshLogin).toBe(false)
  // A foreground refresh is not a new sign-in.
  await act(async () => { native.resumed?.({ isActive: true }); await Promise.resolve(); await Promise.resolve() })
  expect(result.current.freshLogin).toBe(false)
  unmount()
})

it("never treats a restart with a cached session as a fresh sign-in", async () => {
  stubGateway()
  saveGatewaySession(session)
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await Promise.resolve(); await Promise.resolve() })
  expect(result.current.status).toBe("authorized")
  expect(result.current.hosts).toHaveLength(1)
  expect(result.current.freshLogin).toBe(false)
  unmount()
})

it("forgets a pending fresh sign-in when the account is signed out", async () => {
  stubGateway()
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await result.current.login() })
  await act(async () => { await vi.advanceTimersByTimeAsync(5_000) })
  expect(result.current.freshLogin).toBe(true)
  await act(async () => { await result.current.logout() })
  expect(result.current.freshLogin).toBe(false)
  unmount()
})

it("forgets a pending fresh sign-in when the server address changes", async () => {
  stubGateway()
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await result.current.login() })
  await act(async () => { await vi.advanceTimersByTimeAsync(5_000) })
  expect(result.current.freshLogin).toBe(true)
  act(() => result.current.setOrigin("https://other.example.test"))
  expect(result.current.freshLogin).toBe(false)
  unmount()
})

it("forgets a pending fresh sign-in when another sign-in starts", async () => {
  stubGateway()
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await result.current.login() })
  await act(async () => { await vi.advanceTimersByTimeAsync(5_000) })
  expect(result.current.freshLogin).toBe(true)
  await act(async () => { await result.current.login() })
  expect(result.current.freshLogin).toBe(false)
  unmount()
})

it("does not call a sign-in fresh when its first account read fails, even if a later read succeeds", async () => {
  let failing = true
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/mobile/start")) return response({ device_code: "D".repeat(43), user_code: "abcd-1234-abcd-1234", verification_uri: origin + "/client/api/v1/auth/sso/nosec/start?mobile_code=abcd-1234-abcd-1234", interval: 5, expires_at: new Date(Date.now() + 600_000).toISOString() })
    if (url.endsWith("/auth/mobile/poll")) return response({ status: "authorized", mobile_id: "m1", user: session.user })
    if (url.endsWith("/auth/me")) return response({ user: session.user, device: session.device })
    if (url.endsWith("/bootstrap")) return failing ? new Response("{}", { status: 503 }) : response(bootstrap)
    if (url.includes("/link-hosts")) return response([{ id: "pc-1", enabled: true, online: true, owner_user_id: "u1" }])
    return response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  await act(async () => { await result.current.login() })
  await act(async () => { await vi.advanceTimersByTimeAsync(5_000) })
  expect(result.current.status).toBe("error")
  expect(result.current.freshLogin).toBe(false)
  failing = false
  await act(async () => { native.resumed?.({ isActive: true }); await Promise.resolve(); await Promise.resolve() })
  await act(async () => { await vi.advanceTimersByTimeAsync(30_000) })
  expect(result.current.status).toBe("authorized")
  expect(result.current.freshLogin).toBe(false)
  unmount()
})
