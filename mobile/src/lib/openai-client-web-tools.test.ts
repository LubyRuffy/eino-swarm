import { beforeEach, describe, expect, it, vi } from "vitest"
import { clearGatewaySession, saveGatewaySession, type GatewaySession } from "./gateway-account"
import { streamCompletion } from "./openai-client"
import type { DirectProvider } from "./direct-provider"
import { saveWebExecution } from "./web-execution"

const origin = "https://gateway.invalid"
const session: GatewaySession = {
  origin, token: "agm_" + "a".repeat(64),
  user: { id: "owner", username: "owner", enabled: true, approval_status: "approved" },
  device: { id: "phone", expires_at: "2099-01-01T00:00:00Z" },
}
const provider: DirectProvider = { id: "personal", label: "Personal", baseURL: "https://model.invalid/v1", apiKey: "personal-secret", api: "chat", model: "model", catalog: ["model"], timeoutSeconds: 30 }
const sse = (...events: unknown[]) => new Response(events.map((event) => "data: " + (typeof event === "string" ? event : JSON.stringify(event)) + "\n\n").join(""), { headers: { "Content-Type": "text/event-stream" } })

beforeEach(() => localStorage.clear())

describe("approved account cloud web execution", () => {
  it.each([
    ["chat", "personal"], ["responses", "personal"],
    ["chat", "gateway"], ["responses", "gateway"],
  ] as const)("executes %s function calls on the account origin and continues the %s model", async (api, source) => {
    saveGatewaySession(session)
    const selected: DirectProvider = source === "gateway"
      ? { ...provider, api, baseURL: origin + "/v1", apiKey: session.token, sourceKind: "gateway" }
      : { ...provider, api }
    const requests: { url: string; init?: RequestInit; body: Record<string, unknown> }[] = []
    let modelCalls = 0
    const transport = vi.fn(async (resource: RequestInfo | URL, init?: RequestInit) => {
      const url = String(resource)
      requests.push({ url, init, body: JSON.parse(String(init?.body ?? "{}")) })
      if (url === origin + "/v1/web/search") return new Response(JSON.stringify({ success: true, data: { provider: "fixture", query: "current information", results: [{ title: "Result", url: "https://public.invalid/", snippet: "Evidence", position: 1 }] } }))
      modelCalls++
      if (modelCalls > 1) return api === "chat"
        ? sse({ choices: [{ delta: { content: "Answer from evidence" }, finish_reason: "stop" }] }, "[DONE]")
        : sse({ type: "response.output_text.delta", delta: "Answer from evidence" }, { type: "response.completed", response: { id: "response-2", output: [] } })
      return api === "chat"
        ? sse({ choices: [{ delta: { tool_calls: [{ index: 0, id: "call-1", type: "function", function: { name: "web_search", arguments: '{"query":"current information"}' } }] }, finish_reason: "tool_calls" }] }, "[DONE]")
        : sse({ type: "response.output_item.added", output_index: 0, item: { type: "function_call", id: "item-1", call_id: "call-1", name: "web_search", arguments: "" } }, { type: "response.function_call_arguments.delta", item_id: "item-1", output_index: 0, delta: '{"query":"current information"}' }, { type: "response.completed", response: { id: "response-1", output: [{ type: "function_call", id: "item-1", call_id: "call-1", name: "web_search", arguments: '{"query":"current information"}' }] } })
    })
    const onDelta = vi.fn()
    await streamCompletion({ provider: selected, model: "model", reasoning: "", turns: [{ role: "user", parts: [{ kind: "text", text: "Use current information" }] }], fetchImpl: transport, onDelta })
    expect(requests.filter((request) => request.url === origin + "/v1/web/search")).toHaveLength(1)
    expect(modelCalls).toBe(2)
    const cloud = requests.find((request) => request.url === origin + "/v1/web/search")!
    expect(new Headers(cloud.init?.headers).get("Authorization")).toBe("Bearer " + session.token)
    expect(cloud.init).toMatchObject({ redirect: "error", credentials: "omit", cache: "no-store" })
    const models = requests.filter((request) => request.url === selected.baseURL + (api === "chat" ? "/chat/completions" : "/responses"))
    expect(models).toHaveLength(2)
    expect(models.every((request) => new Headers(request.init?.headers).get("Authorization") === "Bearer " + selected.apiKey)).toBe(true)
    if (source === "personal") expect(JSON.stringify(models)).not.toContain(session.token)
    expect(JSON.stringify(models.map((request) => request.body))).not.toContain(session.token)
    expect(models[0].body.tools).toEqual(expect.arrayContaining([expect.objectContaining({ type: "function" })]))
    expect(JSON.stringify(models[1].body)).toContain("call-1")
    expect(JSON.stringify(models[1].body)).toContain("Evidence")
    expect(onDelta).toHaveBeenLastCalledWith({ text: "Answer from evidence", reasoning: "" })
  })

  it.each(["anonymous", "off"])("keeps ordinary direct model chat for %s use", async (mode) => {
    if (mode === "off") { saveGatewaySession(session); saveWebExecution("off") }
    const fetcher = vi.fn(async (_resource: RequestInfo | URL, options?: RequestInit) => {
      expect(JSON.parse(String(options?.body)).tools).toBeUndefined()
      return sse({ choices: [{ delta: { content: "Ordinary answer" }, finish_reason: "stop" }] }, "[DONE]")
    })
    await streamCompletion({ provider, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it("does not dispatch a model request for an already stopped turn", async () => {
    saveGatewaySession(session)
    const controller = new AbortController(); controller.abort("stop")
    const fetcher = vi.fn(async () => sse("[DONE]"))
    await expect(streamCompletion({ provider, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, signal: controller.signal, onDelta: vi.fn() })).rejects.toMatchObject({ kind: "abort" })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it("validates the entire model round before any external cloud call", async () => {
    saveGatewaySession(session)
    const fetcher = vi.fn(async () => sse({ choices: [{ delta: { tool_calls: [
      { index: 0, id: "one", type: "function", function: { name: "web_search", arguments: '{"query":"valid"}' } },
      { index: 1, id: "two", type: "function", function: { name: "web_search", arguments: '{"query":1}' } },
    ] }, finish_reason: "tool_calls" }] }, "[DONE]"))
    await expect(streamCompletion({ provider, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_arguments" })
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it("fails a revoked cloud grant without local work or another model request", async () => {
    saveGatewaySession(session)
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => String(resource).includes("/web/search")
      ? new Response(JSON.stringify({ error: { message: session.token } }), { status: 403 })
      : sse({ choices: [{ delta: { tool_calls: [{ index: 0, id: "one", type: "function", function: { name: "web_search", arguments: '{"query":"x"}' } }] }, finish_reason: "tool_calls" }] }, "[DONE]"))
    await expect(streamCompletion({ provider, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_denied" })
    expect(fetcher).toHaveBeenCalledTimes(2)
  })

  it.each(["Stop", "logout", "account replacement", "location change"])("cancels a pending cloud call on %s and does not continue the model", async (cause) => {
    saveGatewaySession(session)
    const controller = new AbortController()
    let opened!: () => void
    const started = new Promise<void>((resolve) => { opened = resolve })
    let cloudSignal: AbortSignal | undefined
    const fetcher = vi.fn((resource: RequestInfo | URL, options?: RequestInit): Promise<Response> => {
      if (!String(resource).includes("/web/search")) return Promise.resolve(sse({ choices: [{ delta: { tool_calls: [{ index: 0, id: "one", type: "function", function: { name: "web_search", arguments: '{"query":"x"}' } }] }, finish_reason: "tool_calls" }] }, "[DONE]"))
      return new Promise((_resolve, reject) => {
        cloudSignal = options?.signal ?? undefined; opened()
        cloudSignal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")))
      })
    })
    const onTool = vi.fn()
    const result = streamCompletion({ provider, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, signal: controller.signal, onDelta: vi.fn(), onTool })
    const assertion = expect(result).rejects.toMatchObject({ kind: "abort" })
    await started
    if (cause === "Stop") controller.abort("stop")
    else if (cause === "logout") clearGatewaySession()
    else if (cause === "account replacement") saveGatewaySession({ ...session, token: "agm_" + "b".repeat(64), user: { ...session.user, id: "other" } })
    else saveWebExecution("off")
    await assertion
    expect(cloudSignal?.aborted).toBe(true)
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(onTool).toHaveBeenLastCalledWith(expect.objectContaining({ status: "cancelled" }))
  })

  it("stops before the last allowed model round can execute unconsumable tool output", async () => {
    saveGatewaySession(session)
    let models = 0, tools = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { tools++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      models++
      return sse({ choices: [{ delta: { tool_calls: [{ index: 0, id: "call-" + models, type: "function", function: { name: "web_search", arguments: '{"query":"x"}' } }] }, finish_reason: "tool_calls" }] }, "[DONE]")
    })
    await expect(streamCompletion({ provider, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_limit" })
    expect(models).toBe(8)
    expect(tools).toBe(7)
  })

  it("does not switch Responses to Chat after a cloud tool has executed", async () => {
    saveGatewaySession(session)
    let models = 0
    const urls: string[] = []
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      const url = String(resource); urls.push(url)
      if (url.includes("/web/search")) return new Response(JSON.stringify({ success: true, data: { results: [] } }))
      models++
      if (models > 1) return new Response("Not Found", { status: 404 })
      return sse({ type: "response.completed", response: { id: "response-1", output: [{ type: "function_call", call_id: "call-1", name: "web_search", arguments: '{"query":"x"}' }] } })
    })
    await expect(streamCompletion({ provider: { ...provider, api: "responses" }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ kind: "http" })
    expect(models).toBe(2)
    expect(urls.some((url) => url.endsWith("/chat/completions"))).toBe(false)
  })
})

describe("cloud tool terminal and replay receipts", () => {
  function frame(api: "chat" | "responses", responseID: string, callID: string, query = "x") {
    return api === "chat"
      ? { ...(responseID ? { id: responseID } : {}), choices: [{ delta: { tool_calls: [{ index: 0, id: callID, type: "function", function: { name: "web_search", arguments: JSON.stringify({ query }) } }] }, finish_reason: "tool_calls" }] }
      : { type: "response.completed", response: { ...(responseID ? { id: responseID } : {}), status: "completed", output: [{ type: "function_call", call_id: callID, name: "web_search", arguments: JSON.stringify({ query }) }] } }
  }

  it.each(["chat", "responses"] as const)("does not execute a replayed %s response receipt twice", async (api) => {
    saveGatewaySession(session)
    let cloud = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      return sse(frame(api, "same-response", "same-call"), "[DONE]")
    })
    await expect(streamCompletion({ provider: { ...provider, api }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_replay" })
    expect(cloud).toBe(1)
  })

  it.each(["chat", "responses"] as const)("allows an identical call id in distinct %s responses and keeps separate UI receipts", async (api) => {
    saveGatewaySession(session)
    let cloud = 0, models = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      models++
      if (models < 3) return sse(frame(api, "response-" + models, "same-call", "query-" + models), "[DONE]")
      return api === "chat" ? sse({ id: "response-3", choices: [{ delta: { content: "Done" }, finish_reason: "stop" }] }, "[DONE]") : sse({ type: "response.output_text.delta", delta: "Done" }, { type: "response.completed", response: { id: "response-3", output: [] } })
    })
    const onTool = vi.fn()
    await streamCompletion({ provider: { ...provider, api }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn(), onTool })
    expect(cloud).toBe(2)
    const receipts = onTool.mock.calls.map(([event]) => event).filter((event) => event.status === "completed")
    expect(new Set(receipts.map((event) => event.id)).size).toBe(2)
    expect(receipts.map((event) => event.callId)).toEqual(["same-call", "same-call"])
  })

  it("refuses ambiguous reuse of a call id when the Chat provider omits completion identity", async () => {
    saveGatewaySession(session)
    let cloud = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      return sse(frame("chat", "", "same-call"), "[DONE]")
    })
    await expect(streamCompletion({ provider, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_replay" })
    expect(cloud).toBe(1)
  })

  for (const api of ["chat", "responses"] as const) {
    it.each(["known-to-missing", "missing-to-known"])(`${api}: does not lose receipt ambiguity across %s response identity`, async (mode) => {
      saveGatewaySession(session)
      let cloud = 0, models = 0
      const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
        if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
        models++
        const responseID = mode === "known-to-missing" ? models === 1 ? "known" : "" : models === 1 ? "" : "known"
        return sse(frame(api, responseID, "same-call"), "[DONE]")
      })
      await expect(streamCompletion({ provider: { ...provider, api }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_replay" })
      expect(cloud).toBe(1)
    })
  }

  it.each(["chat", "responses"] as const)("does not execute %s function data after DONE", async (api) => {
    saveGatewaySession(session)
    let cloud = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      return sse("[DONE]", frame(api, "late-response", "late-call"))
    })
    await streamCompletion({ provider: { ...provider, api }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })
    expect(cloud).toBe(0)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it.each(["chat", "responses"] as const)("never adds %s calls after the model's endpoint terminal", async (api) => {
    saveGatewaySession(session)
    let cloud = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      const terminal = api === "chat" ? { id: "response", choices: [{ delta: {}, finish_reason: "stop" }] } : { type: "response.completed", response: { id: "response", status: "completed", output: [] } }
      return sse(terminal, frame(api, "response", "late-call"), "[DONE]")
    })
    const result = streamCompletion({ provider: { ...provider, api }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })
    if (api === "chat") await expect(result).rejects.toMatchObject({ message: "cloud_tools_incomplete" })
    else await result
    expect(cloud).toBe(0)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it("does not execute a Responses call omitted from its authoritative terminal output", async () => {
    saveGatewaySession(session)
    let cloud = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      return sse({ type: "response.output_item.added", output_index: 0, item: { type: "function_call", id: "item", call_id: "call", name: "web_search", arguments: '{"query":"x"}' } }, { type: "response.completed", response: { id: "response", status: "completed", output: [] } })
    })
    await expect(streamCompletion({ provider: { ...provider, api: "responses" }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_incomplete" })
    expect(cloud).toBe(0)
  })

  it("does not execute a Responses call whose terminal identity conflicts with its streamed identity", async () => {
    saveGatewaySession(session)
    let cloud = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      return sse({ type: "response.output_item.added", output_index: 0, item: { type: "function_call", id: "item", call_id: "original", name: "web_search", arguments: '{"query":"x"}' } }, frame("responses", "response", "changed"))
    })
    await expect(streamCompletion({ provider: { ...provider, api: "responses" }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ kind: "parse" })
    expect(cloud).toBe(0)
  })

  it.each(["name", "call_id"])("does not borrow a missing Responses terminal %s from earlier streamed data", async (field) => {
    saveGatewaySession(session)
    let cloud = 0
    const fetcher = vi.fn(async (resource: RequestInfo | URL) => {
      if (String(resource).includes("/web/search")) { cloud++; return new Response(JSON.stringify({ success: true, data: { results: [] } })) }
      const final = { type: "function_call", call_id: "call", name: "web_search", arguments: '{"query":"x"}' } as Record<string, unknown>
      delete final[field]
      return sse({ type: "response.output_item.added", output_index: 0, item: { ...final, name: "web_search", call_id: "call" } }, { type: "response.completed", response: { id: "response", status: "completed", output: [final] } })
    })
    await expect(streamCompletion({ provider: { ...provider, api: "responses" }, model: "model", reasoning: "", turns: [], fetchImpl: fetcher, onDelta: vi.fn() })).rejects.toMatchObject({ message: "cloud_tools_arguments" })
    expect(cloud).toBe(0)
  })
})
