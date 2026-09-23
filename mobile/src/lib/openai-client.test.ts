import { describe, expect, it, vi } from "vitest"

import { blankProvider, type DirectProvider } from "./direct-provider"
import { discoverModels, streamCompletion } from "./openai-client"
import { ModelCallError, type StreamPiece } from "./openai-wire"
import type { StreamBodyPlugin } from "./stream-body"

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
    expect(fetchImpl.mock.calls[0][1]?.headers).toMatchObject({ "Accept-Encoding": "identity" })
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
    expect(sent.reasoning).toEqual({ effort: "high", summary: "auto" })
    expect(String(fetchImpl.mock.calls[0][0])).toContain("/responses")
  })

  it("sends an image as chat completions when responses rejects the part", async () => {
    const fetchImpl = vi.fn(async (url: RequestInfo | URL, _init?: RequestInit) => {
      if (String(url).endsWith("/responses")) {
        return jsonResponse(
          { error: { message: 'content part type "input_image" has no Chat Completions equivalent' } },
          400,
        )
      }
      return jsonResponse({ choices: [{ message: { content: "pong" } }] })
    })
    let got = ""
    await streamCompletion({
      provider: provider({ api: "responses" }),
      model: "m",
      reasoning: "low",
      turns: [
        {
          role: "user",
          parts: [
            { kind: "text", text: "see" },
            { kind: "image", mediaType: "image/png", dataUrl: "data:image/png;base64,aa" },
          ],
        },
      ],
      fetchImpl,
      onDelta: (piece) => {
        got = piece.text
      },
    })
    expect(got).toBe("pong")
    expect(fetchImpl).toHaveBeenCalledTimes(2)
    expect(String(fetchImpl.mock.calls[1][0])).toContain("/chat/completions")
    const sent = JSON.parse(String(fetchImpl.mock.calls[1][1]?.body))
    expect(JSON.stringify(sent)).not.toContain("input_image")
    expect(sent.messages[0].content.map((part: { type: string }) => part.type)).toEqual([
      "text",
      "image_url",
    ])
    expect(sent.reasoning_effort).toBe("low")
    expect(sent).not.toHaveProperty("reasoning")
  })

  it("drops a rejected reasoning summary and stays on responses", async () => {
    let calls = 0
    const fetchImpl = vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
      calls += 1
      if (calls === 1) {
        return jsonResponse(
          { error: { message: 'field "reasoning.summary" has no Chat Completions equivalent' } },
          400,
        )
      }
      expect(String(url)).toContain("/responses")
      const sent = JSON.parse(String(init?.body))
      expect(sent.reasoning).toEqual({ effort: "low" })
      return jsonResponse({
        output: [{ type: "message", content: [{ type: "output_text", text: "pong" }] }],
      })
    })
    let got = ""
    await streamCompletion({
      provider: provider({ api: "responses" }),
      model: "m",
      reasoning: "low",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl,
      onDelta: (piece) => {
        got = piece.text
      },
    })
    expect(got).toBe("pong")
    expect(calls).toBe(2)
    expect(String(fetchImpl.mock.calls[1][0])).not.toContain("/chat/completions")
  })

  it("does not retry a responses rejection that is not a missing chat equivalent", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ error: { message: "model" } }, 400))
    await expect(
      streamCompletion({
        provider: provider({ api: "responses" }),
        model: "m",
        reasoning: "",
        turns: [
          {
            role: "user",
            parts: [{ kind: "image", mediaType: "image/png", dataUrl: "data:image/png;base64,aa" }],
          },
        ],
        fetchImpl,
        onDelta: () => undefined,
      }),
    ).rejects.toMatchObject({ kind: "http", message: "model" })
    expect(fetchImpl).toHaveBeenCalledTimes(1)
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

  it("paints each token on its own turn, the thought before the answer", async () => {
    const encoder = new TextEncoder()
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(
          encoder.encode(
            'data: {"choices":[{"delta":{"reasoning_content":"th"}}]}\n\n' +
              'data: {"choices":[{"delta":{"content":"pong"}}]}\n\n' +
              "data: [DONE]\n\n",
          ),
        )
        controller.close()
      },
    })
    const fetchImpl = vi.fn(async () => new Response(stream))
    const marks: string[] = []
    await streamCompletion({
      provider: provider(),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl,
      onDelta: (piece) => {
        marks.push(`delta:${piece.reasoning}:${piece.text}`)
        queueMicrotask(() => marks.push("micro"))
      },
    })
    expect(marks).toEqual(["delta:th:", "micro", "delta:th:pong", "micro"])
  })

  it("shows a thought wrapped in tags, and keeps a trailing angle bracket", async () => {
    const encoder = new TextEncoder()
    const frame = (text: string) =>
      encoder.encode(`data: {"choices":[{"delta":{"content":${JSON.stringify(text)}}}]}\n\n`)
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(frame("<think>th</think>po"))
        controller.enqueue(frame("ng"))
        controller.enqueue(encoder.encode("data: [DONE]\n\n"))
        controller.close()
      },
    })
    const seen: StreamPiece[] = []
    await streamCompletion({
      provider: provider(),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl: vi.fn(async () => new Response(stream)),
      onDelta: (piece) => seen.push({ text: piece.text, reasoning: piece.reasoning }),
    })
    expect(seen[0]).toEqual({ text: "", reasoning: "th" })
    expect(seen.at(-1)).toEqual({ text: "pong", reasoning: "th" })

    const plain = new Response(JSON.stringify({ choices: [{ message: { content: "a<" } }] }), {
      headers: { "Content-Type": "application/json" },
    })
    let got = ""
    await streamCompletion({
      provider: provider(),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      fetchImpl: vi.fn(async () => plain),
      onDelta: (piece) => {
        got = piece.text
      },
    })
    expect(got).toBe("a<")
  })

  it("reads device chunks before the call returns, and surfaces its error body", async () => {
    const seen: string[] = []
    const native = scriptedNative(async (_opts, emit) => {
      emit.status(200)
      emit.chunk('data: {"choices":[{"delta":{"content":"pon"}}]}\n\n')
      await new Promise((resolve) => setTimeout(resolve, 0))
      if (!seen.includes("pon")) throw new Error("buffered")
      emit.chunk('data: {"choices":[{"delta":{"content":"g"}}]}\n\ndata: [DONE]\n\n')
      return { status: 200 }
    })
    await streamCompletion({
      provider: provider(),
      model: "m",
      reasoning: "",
      turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
      nativeStream: native,
      onDelta: (piece) => seen.push(piece.text),
    })
    expect(seen).toEqual(["pon", "pong"])

    const refused = scriptedNative(async (_opts, emit) => {
      emit.status(401)
      emit.chunk('{"error":{"message":"nope sk-test"}}')
      return { status: 401 }
    })
    await expect(
      streamCompletion({
        provider: provider({ apiKey: "sk-test" }),
        model: "m",
        reasoning: "",
        turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
        nativeStream: refused,
        onDelta: () => undefined,
      }),
    ).rejects.toMatchObject({ kind: "http", message: "nope •••" })

    const timedOut = scriptedNative(async () => {
      throw new Error("timeout")
    })
    await expect(
      streamCompletion({
        provider: provider(),
        model: "m",
        reasoning: "",
        turns: [{ role: "user", parts: [{ kind: "text", text: "ping" }] }],
        nativeStream: timedOut,
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

function scriptedNative(
  run: (
    opts: { id: string },
    emit: { status(status: number): void; chunk(text: string): void },
  ) => Promise<{ status?: number; aborted?: boolean }>,
): StreamBodyPlugin {
  const listeners: Record<string, ((ev: { id?: string; status?: number; text?: string }) => void)[]> = {
    status: [],
    chunk: [],
  }
  return {
    async addListener(eventName, listener) {
      listeners[eventName].push(listener)
      return { async remove() {} }
    },
    async cancel() {},
    open(opts) {
      return run(opts, {
        status(status) {
          for (const cb of listeners.status) cb({ id: opts.id, status })
        },
        chunk(text) {
          for (const cb of listeners.chunk) cb({ id: opts.id, text })
        },
      })
    },
  }
}
