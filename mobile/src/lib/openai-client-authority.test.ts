import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { clearGatewaySession, loadGatewaySession, saveGatewaySession, type GatewaySession } from "./gateway-account"
import { discoverModels, streamCompletion } from "./openai-client"
import type { DirectProvider } from "./direct-provider"
import { saveWebExecution } from "./web-execution"
import type { StreamBodyPlugin } from "./stream-body"

const session: GatewaySession = {
  origin: "https://gateway.invalid", token: "agm_" + "a".repeat(64),
  user: { id: "owner", username: "owner", approval_status: "approved", enabled: true },
  device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" },
}
const provider: DirectProvider = { id: "gateway:owner", sourceKind: "gateway", label: "Server", baseURL: session.origin + "/v1", apiKey: session.token, api: "chat", model: "model", catalog: ["model"], timeoutSeconds: 30 }
const request = (row = provider) => ({ provider: row, model: "model", reasoning: "", turns: [], onDelta: vi.fn() })
const denied = (status: number) => new Response(JSON.stringify({ error: { message: "Access refused" } }), { status })

beforeEach(() => { localStorage.clear(); saveGatewaySession(session); saveWebExecution("off") })
afterEach(() => { vi.unstubAllGlobals() })

describe("Gateway model authority independently of Web tools", () => {
  it.each(["models", "completion"])("a current %s 401 immediately expires only the Gateway session and retains HTTP status", async (mode) => {
    const fetcher = vi.fn(async () => denied(401))
    const result = mode === "models" ? discoverModels(provider, fetcher) : streamCompletion({ ...request(), fetchImpl: fetcher })
    await expect(result).rejects.toMatchObject({ kind: "http", status: 401 })
    expect(loadGatewaySession()).toBeNull()
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it.each(["models", "completion"])("a personal provider %s 401 leaves the Gateway account intact", async (mode) => {
    const row = { ...provider, id: "personal", sourceKind: "phone" as const, baseURL: "https://model.invalid/v1", apiKey: "personal-secret" }
    const fetcher = vi.fn(async () => denied(401))
    const result = mode === "models" ? discoverModels(row, fetcher) : streamCompletion({ ...request(row), fetchImpl: fetcher })
    await expect(result).rejects.toMatchObject({ kind: "http", status: 401 })
    expect(loadGatewaySession()?.token).toBe(session.token)
  })

  it("a late 401 cannot erase a newer login", async () => {
    let release!: (response: Response) => void
    const fetcher = vi.fn(() => new Promise<Response>((resolve) => { release = resolve }))
    const result = streamCompletion({ ...request(), fetchImpl: fetcher })
    const assertion = expect(result).rejects.toMatchObject({ kind: "abort" })
    saveGatewaySession({ ...session, token: "agm_" + "b".repeat(64) })
    release(denied(401))
    await assertion
    expect(loadGatewaySession()?.token).toBe("agm_" + "b".repeat(64))
  })

  it.each(["logout", "replacement"])("Web-off inference aborts its pending HTTP request on %s", async (cause) => {
    const stop = new AbortController()
    let modelSignal: AbortSignal | undefined
    const fetcher = vi.fn((_resource: RequestInfo | URL, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
      modelSignal = init?.signal ?? undefined
      modelSignal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")))
    }))
    const result = streamCompletion({ ...request(), signal: stop.signal, fetchImpl: fetcher }).catch((error: unknown) => error)
    try {
      if (cause === "logout") clearGatewaySession()
      else saveGatewaySession({ ...session, token: "agm_" + "b".repeat(64) })
      expect(modelSignal?.aborted).toBe(true)
      await expect(result).resolves.toMatchObject({ kind: "abort" })
    } finally { stop.abort("cleanup"); await result }
  })

  it("rejects a stale Gateway provider before disclosing its old token", async () => {
    saveGatewaySession({ ...session, token: "agm_" + "b".repeat(64) })
    const fetcher = vi.fn(async () => new Response(JSON.stringify({ choices: [{ message: { content: "ok" } }] })))
    await expect(streamCompletion({ ...request(), fetchImpl: fetcher })).rejects.toMatchObject({ message: "gateway_login_required" })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it.each(["models", "completion"])("a %s authentication refusal invalidates authority before an unbounded error body finishes", async (mode) => {
    let finish!: () => void
    let cancelled = false
    const body = new ReadableStream<Uint8Array>({ start(controller) { finish = () => controller.close() }, cancel() { cancelled = true } })
    const stop = new AbortController()
    const fetcher = vi.fn(async () => new Response(body, { status: 401 }))
    const result = (mode === "models" ? discoverModels(provider, fetcher) : streamCompletion({ ...request(), signal: stop.signal, fetchImpl: fetcher })).catch((error: unknown) => error)
    try {
      await Promise.resolve(); await Promise.resolve(); await Promise.resolve()
      expect(loadGatewaySession()).toBeNull()
      await expect(result).resolves.toMatchObject({ kind: "http", status: 401, message: "gateway_login_required" })
      expect(cancelled).toBe(true)
    } finally { stop.abort("cleanup"); if (!cancelled) finish(); await result }
  })

  it.each([
    [{ error: { param: "tools", code: "unsupported_parameter", message: "Unsupported parameter" } }, "cloud_tools_model_unsupported"],
    [{ error: { message: "This model does not support tool calling" } }, "cloud_tools_model_unsupported"],
    [{ error: { message: "tools contains an invalid schema" } }, "tools contains an invalid schema"],
  ])("only classifies explicit model tool capability errors and never retries without tools", async (body, message) => {
    saveWebExecution("auto")
    const personal = { ...provider, id: "personal", sourceKind: "phone" as const, baseURL: "https://model.invalid/v1", apiKey: "personal-secret" }
    const fetcher = vi.fn(async () => new Response(JSON.stringify(body), { status: 400 }))
    await expect(streamCompletion({ ...request(personal), fetchImpl: fetcher })).rejects.toMatchObject({ kind: "http", status: 400, message })
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(loadGatewaySession()?.token).toBe(session.token)
  })

  it.each([401, 403])("a native %s header releases the plugin before any error body and retains the HTTP refusal", async (status) => {
    const listeners: Partial<Record<"status" | "chunk", (event: { id?: string; status?: number; text?: string }) => void>> = {}
    const removed = vi.fn()
    let complete!: (result: { status: number; aborted: boolean }) => void
    const native: StreamBodyPlugin = {
      addListener: vi.fn<StreamBodyPlugin["addListener"]>(async (event, callback) => { listeners[event] = callback; return { remove: async () => { removed(event) } } }),
      open: vi.fn<StreamBodyPlugin["open"]>((options) => new Promise((resolve) => { complete = resolve; listeners.status?.({ id: options.id, status }) })),
      cancel: vi.fn(async () => { complete({ status, aborted: true }) }),
    }
    await expect(streamCompletion({ ...request(), nativeStream: native })).rejects.toMatchObject({ kind: "http", status, message: status === 401 ? "gateway_login_required" : "gateway_access_denied" })
    expect(native.cancel).toHaveBeenCalledTimes(1)
    expect(removed.mock.calls.map(([event]) => event).sort()).toEqual(["chunk", "status"])
    expect(loadGatewaySession()?.token ?? null).toBe(status === 401 ? null : session.token)
  })
})
