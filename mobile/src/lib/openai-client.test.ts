import { describe, expect, it, vi } from "vitest"

import { blankProvider, type DirectProvider } from "./direct-provider"
import { discoverModels, streamCompletion } from "./openai-client"
import { ModelCallError } from "./openai-wire"

function provider(patch: Partial<DirectProvider> = {}): DirectProvider {
  return { ...blankProvider(), baseURL: "https://endpoint.invalid/v1", model: "m", ...patch }
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

describe("openai client", () => {
  it("lists model ids from the endpoint", async () => {
    const fetchImpl = vi.fn(async (_url: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse({ data: [{ id: "a" }, { id: "b" }] }),
    )
    await expect(discoverModels(provider(), fetchImpl)).resolves.toEqual(["a", "b"])
    expect(String(fetchImpl.mock.calls[0][0])).toBe("https://endpoint.invalid/v1/models")
    expect(fetchImpl.mock.calls[0][1]?.headers).toMatchObject({ Accept: expect.any(String) })
  })

  it("sends the key and refuses to echo it back in an error", async () => {
    const fetchImpl = vi.fn(async (_url: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse({ error: { message: "bad key sk-test" } }, 401),
    )
    const row = provider({ apiKey: "sk-test" })
    await expect(discoverModels(row, fetchImpl)).rejects.toMatchObject({
      kind: "http",
      message: "bad key •••",
    })
    expect(fetchImpl.mock.calls[0][1]?.headers).toMatchObject({
      Authorization: "Bearer sk-test",
    })
  })

  it("streams chat deltas and does not invent a system turn", async () => {
    const encoder = new TextEncoder()
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(
          encoder.encode('data: {"choices":[{"delta":{"content":"pon"}}]}\n\n'),
        )
        controller.enqueue(
          encoder.encode('data: {"choices":[{"delta":{"content":"g"}}]}\n\ndata: [DONE]\n\n'),
        )
        controller.close()
      },
    })
    const fetchImpl = vi.fn(async (_url: RequestInfo | URL, _init?: RequestInit) =>
      new Response(stream, { headers: { "Content-Type": "text/event-stream" } }),
    )
    const seen: string[] = []
    await streamCompletion({
      provider: provider(),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl,
      onDelta: (piece) => seen.push(piece.text),
    })
    expect(seen).toEqual(["pon", "pong"])
    const sent = JSON.parse(String(fetchImpl.mock.calls[0][1]?.body))
    expect(sent.messages).toEqual([{ role: "user", content: "ping" }])
    expect(sent).not.toHaveProperty("reasoning_effort")
    expect(String(fetchImpl.mock.calls[0][0])).toContain("/chat/completions")
  })

  it("posts the responses wire and reads a single JSON body", async () => {
    const fetchImpl = vi.fn(async (_url: RequestInfo | URL, _init?: RequestInit) =>
      jsonResponse({
        output: [{ type: "message", content: [{ type: "output_text", text: "pong" }] }],
      }),
    )
    let got = ""
    await streamCompletion({
      provider: provider({ api: "responses" }),
      model: "m",
      reasoning: "high",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl,
      onDelta: (piece) => {
        got = piece.text
      },
    })
    expect(got).toBe("pong")
    const sent = JSON.parse(String(fetchImpl.mock.calls[0][1]?.body))
    expect(sent.reasoning).toEqual({ effort: "high" })
    expect(String(fetchImpl.mock.calls[0][0])).toContain("/responses")
  })

  it("does not concatenate a gateway that resends the whole buffer", async () => {
    const sentence = "abcdefghijkl"
    const encoder = new TextEncoder()
    const stream = new ReadableStream({
      start(controller) {
        const frame = (text: string) =>
          encoder.encode(`data: {"choices":[{"delta":{"content":${JSON.stringify(text)}}}]}\n\n`)
        controller.enqueue(frame(sentence))
        controller.enqueue(frame(sentence))
        controller.enqueue(frame(sentence + "m"))
        controller.enqueue(encoder.encode("data: [DONE]\n\n"))
        controller.close()
      },
    })
    const fetchImpl = vi.fn(async () => new Response(stream))
    const seen: string[] = []
    await streamCompletion({
      provider: provider(),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl,
      onDelta: (piece) => seen.push(piece.text),
    })
    expect(seen).toEqual([sentence, sentence + "m"])
  })

  it("reads a JSON answer that mentions data:", async () => {
    const fetchImpl = vi.fn(async () =>
      jsonResponse({ choices: [{ message: { content: "see data: here" } }] }),
    )
    let got = ""
    await streamCompletion({
      provider: provider(),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl,
      onDelta: (piece) => {
        got = piece.text
      },
    })
    expect(got).toBe("see data: here")
  })

  it("reads a responses stream that only sends the completed snapshot", async () => {
    const payload = JSON.stringify({
      type: "response.completed",
      response: {
        output: [{ type: "message", content: [{ type: "output_text", text: "pong" }] }],
      },
    })
    const fetchImpl = vi.fn(async () => new Response(`data: ${payload}\n\n`))
    let got = ""
    await streamCompletion({
      provider: provider({ api: "responses" }),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl,
      onDelta: (piece) => {
        got = piece.text
      },
    })
    expect(got).toBe("pong")
  })

  it("fails when the socket drops instead of ending the reply", async () => {
    const stream = new ReadableStream({
      start(controller) {
        controller.error(new Error("reset"))
      },
    })
    const fetchImpl = vi.fn(async () => new Response(stream))
    await expect(
      streamCompletion({
        provider: provider(),
        model: "m",
        reasoning: "",
        turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
        fetchImpl,
        onDelta: () => undefined,
      }),
    ).rejects.toMatchObject({ kind: "http", message: "network" })
  })

  it("aborts when the connection never answers", async () => {
    const fetchImpl = vi.fn(
      (_url: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => {
            reject(new DOMException("aborted", "AbortError"))
          })
        }),
    )
    const row = provider()
    Object.defineProperty(row, "timeoutSeconds", { value: 0.02 })
    await expect(
      streamCompletion({
        provider: row,
        model: "m",
        reasoning: "",
        turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
        fetchImpl,
        onDelta: () => undefined,
      }),
    ).rejects.toMatchObject({ kind: "timeout" })
  })

  it("aborts when the endpoint stays silent", async () => {
    const stream = new ReadableStream({
      start() {
        // never enqueue
      },
    })
    const fetchImpl = vi.fn(async () => new Response(stream))
    const row = provider({ timeoutSeconds: 10 })
    Object.defineProperty(row, "timeoutSeconds", { value: 0.02 })
    await expect(
      streamCompletion({
        provider: row,
        model: "m",
        reasoning: "",
        turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
        fetchImpl,
        onDelta: () => undefined,
      }),
    ).rejects.toBeInstanceOf(ModelCallError)
  })
})
