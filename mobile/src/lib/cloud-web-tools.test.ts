import { beforeEach, expect, it, vi } from "vitest"
import { cloudToolTraceText, currentCloudWebTools, executeCloudWebTool, type CloudToolCall } from "./cloud-web-tools"
import { saveGatewaySession, type GatewaySession } from "./gateway-account"
import { loadWebExecution, pcWebExecution, saveWebExecution } from "./web-execution"

const session: GatewaySession = { origin: "https://account.invalid", token: "agm_" + "b".repeat(64), user: { id: "one", username: "one", enabled: true, approval_status: "approved" }, device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" } }
const fetchCall: CloudToolCall = { id: "call", name: "web_fetch", arguments: '{"url":"https://public.invalid/","render":true}' }
const ok = (data: unknown) => new Response(JSON.stringify({ success: true, data }))
beforeEach(() => { localStorage.clear(); saveGatewaySession(session) })

it("projects search evidence into existing text tool cards without changing the model's result envelope", () => {
  const output = JSON.stringify({ message: "Cloud web search", results: [{ title: "Title", url: "https://public.invalid/", summary: "Visible evidence" }] })
  expect(cloudToolTraceText("web_search", output)).toBe("Title\nhttps://public.invalid/\nVisible evidence")
  expect(cloudToolTraceText("web_fetch", "# Page")).toBe("# Page")
})

it("defaults to cloud only with approval and keeps explicit preferences per account origin and user", () => {
  expect(loadWebExecution()).toBe("auto")
  expect(pcWebExecution()).toBe("")
  saveWebExecution("off")
  expect(currentCloudWebTools()).toBeNull()
  expect(pcWebExecution()).toBe("")
  saveGatewaySession({ ...session, user: { ...session.user, id: "two" } })
  expect(loadWebExecution()).toBe("auto")
  saveWebExecution("cloud")
  saveGatewaySession({ ...session, origin: "https://other.invalid" })
  expect(loadWebExecution()).toBe("auto")
  saveGatewaySession(session)
  expect(loadWebExecution()).toBe("off")
  saveGatewaySession({ ...session, user: { ...session.user, approval_status: "pending" } })
  expect(currentCloudWebTools()).toBeNull()
})

it("uses the approved origin, forces browser rendering when requested, and returns Markdown", async () => {
  const fetcher = vi.fn(async () => ok({ markdown: "# Page" }))
  const tools = currentCloudWebTools(fetcher)!
  expect(await executeCloudWebTool(tools, fetchCall, new AbortController().signal, 1000)).toBe("# Page")
  const [url, options] = fetcher.mock.calls[0] as unknown as [string, RequestInit]
  expect(url).toBe(session.origin + "/v1/web/fetch")
  expect(options).toMatchObject({ redirect: "error", credentials: "omit", cache: "no-store" })
  expect(new Headers(options.headers).get("Authorization")).toBe("Bearer " + session.token)
  expect(JSON.parse(String(options.body))).toEqual({ url: "https://public.invalid/", formats: ["markdown"], actions: [{ type: "wait", milliseconds: 1 }] })
})

it.each([
  ["unknown", "{}"], ["web_fetch", '{"url":"https://name:password@public.invalid/"}'], ["web_fetch", '{"url":"file:///private"}'],
  ["web_fetch", '{"url":"https://public.invalid/","render":"true"}'], ["web_search", '{"query":1}'], ["web_search", '{"query":"x","url":"https://public.invalid/"}'],
  ["web_search", '{"query":"' + "x".repeat(4097) + '"}'], ["web_search", "{"],
])("refuses invalid %s arguments before any HTTP", async (name, args) => {
  const fetcher = vi.fn(async () => ok({ markdown: "unused" }))
  await expect(executeCloudWebTool(currentCloudWebTools(fetcher)!, { id: "call", name, arguments: args }, new AbortController().signal, 1000)).rejects.toMatchObject({ kind: "parse" })
  expect(fetcher).not.toHaveBeenCalled()
})

it.each([401, 403, 503])("returns a bounded safe error for status %d without exposing response credentials", async (status) => {
  const fetcher = vi.fn(async () => new Response(session.token, { status }))
  await expect(executeCloudWebTool(currentCloudWebTools(fetcher)!, fetchCall, new AbortController().signal, 1000)).rejects.toMatchObject({ message: status === 401 ? "cloud_tools_login" : status === 403 ? "cloud_tools_denied" : "cloud_tools_unavailable" })
  expect(fetcher).toHaveBeenCalledTimes(1)
})

it("closes an error response body without waiting for an unbounded download", async () => {
  const cancelled = vi.fn()
  const fetcher = vi.fn(async () => new Response(new ReadableStream({ cancel: cancelled }), { status: 503 }))
  await expect(executeCloudWebTool(currentCloudWebTools(fetcher)!, fetchCall, new AbortController().signal, 1000)).rejects.toMatchObject({ message: "cloud_tools_unavailable" })
  expect(cancelled).toHaveBeenCalledTimes(1)
})

it("refuses an account credential reflected in successful tool output", async () => {
  const fetcher = vi.fn(async () => ok({ markdown: session.token }))
  await expect(executeCloudWebTool(currentCloudWebTools(fetcher)!, fetchCall, new AbortController().signal, 1000)).rejects.toMatchObject({ message: "cloud_tools_invalid" })
})

it.each(["large header", "large body", "large projection", "invalid JSON", "foreign response"])("bounds %s without model continuation", async (mode) => {
  const fetcher = vi.fn(async () => {
    if (mode === "large header") return new Response("{}", { headers: { "Content-Length": String((1 << 20) + 1) } })
    if (mode === "large body") return new Response("x".repeat((1 << 20) + 1))
    if (mode === "large projection") return ok({ markdown: "x".repeat((128 << 10) + 1) })
    if (mode === "invalid JSON") return new Response("{bad")
    const response = ok({ markdown: "x" }); Object.defineProperty(response, "url", { value: "https://other.invalid/" }); return response
  })
  await expect(executeCloudWebTool(currentCloudWebTools(fetcher)!, fetchCall, new AbortController().signal, 1000)).rejects.toBeInstanceOf(Error)
})

it("cancels a tool request on the caller's Stop signal", async () => {
  const controller = new AbortController()
  let opened!: () => void
  const started = new Promise<void>((resolve) => { opened = resolve })
  let requestSignal: AbortSignal | undefined
  const fetcher = vi.fn((_url: RequestInfo | URL, options?: RequestInit) => new Promise<Response>((_resolve, reject) => {
    requestSignal = options?.signal ?? undefined; opened()
    requestSignal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")))
  }))
  const promise = executeCloudWebTool(currentCloudWebTools(fetcher)!, fetchCall, controller.signal, 1000)
  const assertion = expect(promise).rejects.toMatchObject({ kind: "abort" })
  await started; controller.abort("stop"); await assertion
  expect(requestSignal?.aborted).toBe(true)
})

it("times out a tool request independently of provider credentials", async () => {
  const fetcher = vi.fn((_url: RequestInfo | URL, options?: RequestInit) => new Promise<Response>((_resolve, reject) => {
    options?.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")))
  }))
  await expect(executeCloudWebTool(currentCloudWebTools(fetcher)!, fetchCall, new AbortController().signal, 5)).rejects.toMatchObject({ kind: "timeout" })
})


it("refuses explicit cloud before any provider request when signed out or the account is not approved", () => {
  for (const invalid of [null, { ...session, user: { ...session.user, approval_status: "pending" as const } }, { ...session, device: { ...session.device, expires_at: "2000-01-01T00:00:00Z" } }]) {
    localStorage.clear()
    if (invalid) saveGatewaySession(invalid)
    saveWebExecution("cloud")
    expect(() => currentCloudWebTools()).toThrow("gateway_login_required")
  }
})
