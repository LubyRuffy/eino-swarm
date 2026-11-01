import { act, render, renderHook, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { loadGatewaySession, saveGatewaySession, type GatewaySession } from "./gateway-account"
import { notifyGatewayRefusal } from "./gateway-authority"
import { discoverModels } from "./openai-client"
import { useGatewayAccount } from "./use-gateway-account"
import { loadSavedLinks, saveLink } from "./store"
import type { DirectProvider } from "./direct-provider"
import { GatewayAccountShell } from "../components/gateway-account-shell"

vi.mock("@capacitor/app", () => ({ App: { addListener: vi.fn(async () => ({ remove: async () => undefined })) } }))
vi.mock("@capacitor/core", () => ({ Capacitor: { getPlatform: () => "ios", isNativePlatform: () => false }, registerPlugin: () => ({}) }))
vi.mock("@capacitor/browser", () => ({ Browser: { open: vi.fn() } }))

const session: GatewaySession = { origin: "https://gateway.invalid", token: "agm_" + "a".repeat(64), user: { id: "owner", username: "Owner", approval_status: "approved", enabled: true }, device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" } }
const provider: DirectProvider = { id: "gateway:owner", sourceKind: "gateway", label: "Server", baseURL: session.origin + "/v1", apiKey: session.token, api: "chat", model: "model", catalog: ["model"], timeoutSeconds: 30 }
const response = (data: unknown) => new Response(JSON.stringify({ data }))
const bootstrap = (apiAccess: boolean) => ({ user: session.user, device: session.device, models: { source_id: "gateway:owner", source_kind: "gateway", label: "Server", execution: "phone", api_base_url: session.origin + "/v1", catalog_url: session.origin + "/v1/models", api_access_enabled: apiAccess }, pairlink: { enabled: true, hub_url: session.origin, redeem_url: session.origin + "/pairlink/v1/pairings/redeem", websocket_url: session.origin.replace(/^https:/, "wss:") + "/pairlink/v1/ws", info_url: session.origin + "/pairlink/v1/info" }, heartbeat_interval: 30, presence_ttl: 90 })

beforeEach(() => { localStorage.clear(); saveGatewaySession(session) })
afterEach(() => { vi.unstubAllGlobals() })

it.each(["origin", "token", "user", "device"] as const)("a refusal belonging to an old %s cannot revoke a newer login", (changed) => {
  const current = { ...session, ...(changed === "origin" ? { origin: "https://other.invalid" } : changed === "token" ? { token: "agm_" + "b".repeat(64) } : changed === "user" ? { user: { ...session.user, id: "other" } } : { device: { ...session.device, id: "other" } }) }
  saveGatewaySession(current)
  notifyGatewayRefusal(session, 401)
  notifyGatewayRefusal(session, 403)
  expect(loadGatewaySession()).toEqual(current)
})

it("a data-plane 401 updates the mounted account controller immediately", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => url.endsWith("/auth/me") ? response({ user: session.user, device: session.device }) : url.endsWith("/bootstrap") ? response(bootstrap(true)) : url.includes("/link-hosts") ? response([]) : response({ ok: true })))
  const { result, unmount } = renderHook(useGatewayAccount)
  try {
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    expect(result.current.status).toBe("authorized")
    await act(async () => { await expect(discoverModels(provider, vi.fn(async () => new Response("{}", { status: 401 })))).rejects.toMatchObject({ status: 401 }) })
    expect(result.current.status).toBe("expired")
    expect(result.current.session).toBeNull()
    expect(result.current.bootstrap).toBeUndefined()
    expect(result.current.hosts).toEqual([])
  } finally { unmount() }
})

it("a data-plane 403 rechecks privileges without logging out or deleting independent PC ownership", async () => {
  let reads = 0
  let allowed = true
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) { reads++; return response({ user: session.user, device: session.device }) }
    if (url.endsWith("/bootstrap")) return response(bootstrap(allowed))
    return url.includes("/link-hosts") ? response([]) : response({ ok: true })
  }))
  const { result, unmount } = renderHook(useGatewayAccount)
  try {
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    saveLink({ hubURL: session.origin, hostPub: "owned", fingerprint: "owned", ticket: "ticket", sessionID: "session", gatewayOrigin: session.origin, gatewayOwnerID: session.user.id, gatewayHostID: "pc", gatewayMobileID: session.device.id })
    allowed = false
    await act(async () => { await expect(discoverModels(provider, vi.fn(async () => new Response("{}", { status: 403 })))).rejects.toMatchObject({ status: 403 }) })
    expect(reads).toBe(2)
    expect(result.current.status).toBe("authorized")
    expect(result.current.bootstrap?.models.api_access_enabled).toBe(false)
    expect(loadGatewaySession()?.token).toBe(session.token)
    expect(loadSavedLinks().map((link) => link.fingerprint)).toEqual(["owned"])
  } finally { unmount() }
})

it("a refusal recheck discovering pending approval removes managed access and keeps anonymous pairing", async () => {
  let approval = "approved"
  vi.stubGlobal("fetch", vi.fn(async (url: string) => url.endsWith("/auth/me") ? response({ user: { ...session.user, approval_status: approval }, device: session.device }) : url.endsWith("/bootstrap") ? response(bootstrap(true)) : url.includes("/link-hosts") ? response([]) : response({ ok: true })))
  const { result, unmount } = renderHook(useGatewayAccount)
  try {
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    saveLink({ hubURL: session.origin, hostPub: "owned", fingerprint: "owned", ticket: "ticket", sessionID: "session", gatewayOrigin: session.origin, gatewayOwnerID: session.user.id, gatewayHostID: "pc", gatewayMobileID: session.device.id })
    saveLink({ hubURL: "https://anonymous.invalid", hostPub: "anon", fingerprint: "anon", ticket: "t", sessionID: "s" })
    approval = "pending"
    await act(async () => { notifyGatewayRefusal(session, 403); await Promise.resolve(); await Promise.resolve() })
    expect(result.current.status).toBe("account_pending")
    expect(result.current.bootstrap).toBeUndefined()
    expect(result.current.hosts).toEqual([])
    expect(loadSavedLinks().map((link) => link.fingerprint)).toEqual(["anon"])
  } finally { unmount() }
})

it("the mounted normal UI survives a Web-only refusal while authority is rechecked and restored", async () => {
  let reads = 0
  let release!: (response: Response) => void
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) {
      if (++reads === 1) return response({ user: session.user, device: session.device })
      return new Promise<Response>((resolve) => { release = resolve })
    }
    if (url.endsWith("/bootstrap")) return response(bootstrap(true))
    if (url.endsWith("/v1/models")) return new Response(JSON.stringify({ data: [{ id: "model" }] }))
    return url.includes("/link-hosts") ? response([]) : response({ ok: true })
  }))
  const view = render(<GatewayAccountShell><input aria-label="Current conversation" defaultValue="Draft" /></GatewayAccountShell>)
  try {
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    const composer = screen.getByLabelText("Current conversation")
    act(() => { notifyGatewayRefusal(session, 403) })
    expect(screen.getByLabelText("Current conversation")).toBe(composer)
    expect(loadGatewaySession()?.user.approval_status).toBeUndefined()
    const fetcher = vi.fn(async () => new Response("{}"))
    await expect(discoverModels(provider, fetcher)).rejects.toMatchObject({ message: "gateway_login_required" })
    expect(fetcher).not.toHaveBeenCalled()
    await act(async () => { release(response({ user: session.user, device: session.device })); await Promise.resolve(); await Promise.resolve() })
    expect(screen.getByLabelText("Current conversation")).toBe(composer)
    expect(loadGatewaySession()?.user.approval_status).toBe("approved")
  } finally { view.unmount() }
})

it("an unverifiable refusal removes normal UI and a late old recheck cannot overwrite a replacement login", async () => {
  let reads = 0
  let release!: (response: Response) => void
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url.endsWith("/auth/me")) {
      if (++reads === 1) return response({ user: session.user, device: session.device })
      return new Promise<Response>((resolve) => { release = resolve })
    }
    if (url.endsWith("/bootstrap")) return response(bootstrap(true))
    if (url.endsWith("/v1/models")) return new Response(JSON.stringify({ data: [{ id: "model" }] }))
    return url.includes("/link-hosts") ? response([]) : response({ ok: true })
  }))
  const view = render(<GatewayAccountShell><input aria-label="Current conversation" /></GatewayAccountShell>)
  try {
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    act(() => { notifyGatewayRefusal(session, 403) })
    await act(async () => { release(new Response("{}", { status: 503 })); await Promise.resolve(); await Promise.resolve() })
    expect(screen.queryByLabelText("Current conversation")).toBeNull()
    expect(screen.getByTestId("gateway-account")).toBeInTheDocument()
    expect(loadGatewaySession()?.user.approval_status).toBeUndefined()
    // A retry captures the old restricted identity. Its response cannot restore
    // that identity after a newer login has replaced the credential.
    act(() => { notifyGatewayRefusal(session, 403) })
    const replacement = { ...session, token: "agm_" + "b".repeat(64) }
    saveGatewaySession(replacement)
    await act(async () => { release(response({ user: session.user, device: session.device })); await Promise.resolve(); await Promise.resolve() })
    expect(loadGatewaySession()).toEqual(replacement)
  } finally { view.unmount() }
})
