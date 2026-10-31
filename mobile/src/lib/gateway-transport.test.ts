import { readFileSync } from "node:fs"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { logoutGatewaySession, type GatewaySession } from "./gateway-account"
import { discoverModels, streamCompletion } from "./openai-client"
import { blankProvider, type DirectProvider } from "./direct-provider"

const native = vi.hoisted(() => ({ enabled: true }))
vi.mock("@capacitor/core", () => ({
  Capacitor: { isNativePlatform: () => native.enabled, getPlatform: () => native.enabled ? "ios" : "web" },
  registerPlugin: () => ({}),
}))
vi.mock("@capacitor/browser", () => ({ Browser: { open: vi.fn() } }))

const origin = "https://gateway.example.test"
const session: GatewaySession = {
  origin, token: "agm_" + "a".repeat(64), user: { id: "u1", username: "user", approval_status: "approved" },
  device: { id: "m1", expires_at: "2099-01-01T00:00:00Z" },
}
const managed: DirectProvider = {
  ...blankProvider(), id: "gateway:owner", sourceKind: "gateway", baseURL: origin + "/v1", apiKey: session.token, api: "chat", model: "shared",
}
type RawWindow = Window & { CapacitorWebFetch?: typeof fetch }
const rawWindow = window as RawWindow
const json = (data: unknown) => new Response(JSON.stringify(data), { headers: { "Content-Type": "application/json" } })

/** Execute the installed SDK's actual fetch replacement, rather than a
 * hand-written substitute that might accidentally preserve dropped options. */
function patchInstalledSDK(original: typeof fetch, request: (options: unknown) => Promise<unknown>, platform: "ios" | "android") {
  const path = platform === "ios" ? "ios/Capacitor/Capacitor/assets" : "android/capacitor/src/main/assets"
  const source = readFileSync(`node_modules/@capacitor/${path}/native-bridge.js`, "utf8")
  const start = source.indexOf("window.fetch = async (resource, options) => {")
  const end = source.indexOf("window.XMLHttpRequest = function () {", start)
  expect(start).toBeGreaterThan(0)
  expect(end).toBeGreaterThan(start)
  const runtime = { CapacitorWebFetch: original, fetch: original }
  const cap = { getServerUrl: () => "capacitor://localhost", nativePromise: (_plugin: string, _method: string, options: unknown) => request(options) }
  const body = async (data: unknown) => ({ data, type: "text", headers: {} })
  // Node's Request rejects a JSDOM AbortSignal by brand. Preserve that public
  // signal while letting Request normalize the remaining URL/header/body fields.
  class RealmRequest extends Request {
    constructor(resource: RequestInfo | URL, options?: RequestInit) {
      super(resource, { ...options, signal: undefined })
      if (options?.signal) Object.defineProperty(this, "signal", { value: options.signal })
    }
  }
  new Function("window", "win", "cap", "platform", "convertBody", "createProxyUrl", "console", "Request", source.slice(start, end))(
    runtime, runtime, cap, platform, body, () => "https://native-proxy.invalid/", { time() {}, timeEnd() {} }, RealmRequest,
  )
  rawWindow.CapacitorWebFetch = original
  vi.stubGlobal("fetch", runtime.fetch)
}

beforeEach(() => { native.enabled = true; localStorage.clear() })
afterEach(() => { delete rawWindow.CapacitorWebFetch; vi.unstubAllGlobals(); vi.useRealTimers() })

describe.each(["ios", "android"] as const)("Gateway transport through the pinned %s HTTP patch", (platform) => {
  it("uses original WebView fetch for control POST and rejects redirect before native forwarding", async () => {
    const original = vi.fn<typeof fetch>(async (_url, init) => {
      if (init?.redirect === "error") throw new TypeError("redirect refused")
      return json({ data: { ok: true } })
    })
    const forwarded = vi.fn(async () => ({ status: 200, data: { data: { ok: true } }, headers: { "Content-Type": "application/json" }, url: "https://foreign.example.test/accepted" }))
    patchInstalledSDK(original, forwarded, platform)
    await expect(logoutGatewaySession(session)).rejects.toThrow("redirect refused")
    expect(forwarded).not.toHaveBeenCalled()
    expect(original).toHaveBeenCalledWith(origin + "/client/api/v1/auth/logout", expect.objectContaining({ redirect: "error", credentials: "omit", cache: "no-store", signal: expect.any(AbortSignal) }))
  })

  it("cancels the actual control fetch at its fifteen-second deadline instead of leaving the native POST pending", async () => {
    vi.useFakeTimers()
    let aborted = false
    const original = vi.fn<typeof fetch>(async (_url, init) => new Promise((_resolve, reject) => {
      init?.signal?.addEventListener("abort", () => { aborted = true; reject(new DOMException("aborted", "AbortError")) }, { once: true })
    }))
    let release!: (value: unknown) => void
    const forwarded = vi.fn(() => new Promise((resolve) => { release = resolve }))
    patchInstalledSDK(original, forwarded, platform)
    let outcome = "pending"
    const pending = logoutGatewaySession(session).then(() => { outcome = "completed" }, (error: unknown) => { outcome = error instanceof Error ? error.message : "unknown" })
    try {
      await vi.advanceTimersByTimeAsync(15_000)
      expect(outcome).toBe("gateway_timeout")
      expect(aborted).toBe(true)
      expect(forwarded).not.toHaveBeenCalled()
    } finally {
      release?.({ status: 200, data: { data: { ok: true } }, headers: { "Content-Type": "application/json" }, url: origin })
      await pending
    }
  })

  it("fails closed on a native platform whose unpatched WebView fetch is unavailable", async () => {
    const patched = vi.fn<typeof fetch>(async () => json({ data: { ok: true } }))
    vi.stubGlobal("fetch", patched)
    await expect(logoutGatewaySession(session)).rejects.toThrow("gateway_transport_unavailable")
    expect(patched).not.toHaveBeenCalled()
  })

  it("keeps managed model discovery out of the native redirecting GET proxy", async () => {
    const original = vi.fn<typeof fetch>(async (_url, init) => {
      if (init?.redirect === "error") throw new TypeError("redirect refused")
      return json({ data: [{ id: "redirected-model" }] })
    })
    const forwarded = vi.fn(async () => ({}))
    patchInstalledSDK(original, forwarded, platform)
    await expect(discoverModels(managed)).rejects.toMatchObject({ kind: "http", message: "network" })
    expect(original).toHaveBeenCalledOnce()
    expect(original).toHaveBeenCalledWith(origin + "/v1/models", expect.objectContaining({ redirect: "error", credentials: "omit" }))
    expect(forwarded).not.toHaveBeenCalled()
  })

  it("preserves existing discovery routing for a phone's private provider", async () => {
    const original = vi.fn<typeof fetch>(async () => json({ data: [{ id: "private-model" }] }))
    const forwarded = vi.fn(async () => ({}))
    patchInstalledSDK(original, forwarded, platform)
    await expect(discoverModels({ ...managed, id: "p-private", sourceKind: "phone" })).resolves.toEqual(["private-model"])
    expect(original.mock.calls[0][0]).toBe("https://native-proxy.invalid/")
  })

  it("rejects browser redirects only for managed model streams", async () => {
    native.enabled = false
    const fetcher = vi.fn<typeof fetch>(async (_url, init) => {
      if (init?.redirect === "error") throw new TypeError("redirect refused")
      return new Response('data: {"choices":[{"delta":{"content":"private answer"}}]}\n\ndata: [DONE]\n\n')
    })
    const delta = vi.fn()
    const input = { model: "shared", reasoning: "", turns: [{ role: "user" as const, parts: [{ kind: "text" as const, text: "test" }] }], fetchImpl: fetcher, onDelta: delta }
    await expect(streamCompletion({ ...input, provider: managed })).rejects.toMatchObject({ kind: "http", message: "network" })
    expect(delta).not.toHaveBeenCalled()
    expect(fetcher.mock.calls[0][1]).toMatchObject({ redirect: "error", credentials: "omit" })
    await expect(streamCompletion({ ...input, provider: { ...managed, id: "p-private", sourceKind: "phone" } })).resolves.toBeUndefined()
    expect(fetcher.mock.calls[1][1]).not.toHaveProperty("redirect")
    expect(delta).toHaveBeenCalledWith({ text: "private answer", reasoning: "" })
  })
})
