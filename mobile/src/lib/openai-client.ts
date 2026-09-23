import {
  absorbChunk,
  catalogNames,
  chatRequestBody,
  completionURL,
  errorText,
  httpBaseURL,
  looksLikeSSE,
  ModelCallError,
  modelsURL,
  normalizeNewlines,
  peelThought,
  pieceFromData,
  redact,
  replyFromJSON,
  responsesRequestBody,
  takeSSE,
  type ApiStyle,
  type StreamPiece,
  type WireTurn,
} from "./openai-wire"
import type { DirectProvider } from "./direct-provider"
import { defaultTimeoutSeconds } from "./direct-provider"
import { openBody, type StreamBodyPlugin } from "./stream-body"

/** Discover is a button. It must not sit on the provider's five-minute idle clock. */
const discoverCapMs = 30_000

export type FetchLike = typeof fetch

export async function discoverModels(
  row: DirectProvider,
  fetchImpl: FetchLike = fetch,
): Promise<string[]> {
  const base = httpBaseURL(row.baseURL)
  if (!base) throw new ModelCallError("bad-url", "http")
  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort("timeout"), discoverCapMs)
  try {
    const res = await fetchImpl(modelsURL(base), {
      method: "GET",
      headers: authHeaders(row, false),
      signal: ctrl.signal,
    })
    const raw = await res.text()
    if (!res.ok) throw new ModelCallError(redact(errorText(raw, res.status), row.apiKey), "http")
    let parsed: unknown
    try {
      parsed = JSON.parse(raw)
    } catch {
      throw new ModelCallError(redact(errorText(raw, res.status), row.apiKey), "parse")
    }
    const names = catalogNames(parsed)
    if (names.length === 0) throw new ModelCallError("empty", "parse")
    return names
  } catch (err) {
    if (err instanceof ModelCallError) throw err
    if (ctrl.signal.reason === "timeout") throw new ModelCallError("timeout", "timeout")
    throw new ModelCallError("network", "http")
  } finally {
    clearTimeout(timer)
  }
}

export async function streamCompletion(input: {
  provider: DirectProvider
  model: string
  reasoning: string
  turns: WireTurn[]
  signal?: AbortSignal
  fetchImpl?: FetchLike
  nativeStream?: StreamBodyPlugin
  onDelta: (piece: StreamPiece) => void
}): Promise<void> {
  const base = httpBaseURL(input.provider.baseURL)
  if (!base) throw new ModelCallError("bad-url", "http")
  const api = input.provider.api
  const ctrl = new AbortController()
  const stop = () => {
    if (!ctrl.signal.aborted) ctrl.abort(input.signal?.reason ?? "stop")
  }
  input.signal?.addEventListener("abort", stop)
  const seconds =
    input.provider.timeoutSeconds > 0 ? input.provider.timeoutSeconds : defaultTimeoutSeconds
  const idleMs = Math.max(1, Math.round(seconds * 1000))
  // Idle covers the wait for headers too. A socket that never answers
  // used to sit in pending until the OS gave up.
  let idleTimer: ReturnType<typeof setTimeout> | undefined
  const armIdle = () => {
    clearTimeout(idleTimer)
    idleTimer = setTimeout(() => {
      if (!ctrl.signal.aborted) ctrl.abort("timeout")
    }, idleMs)
  }
  const postTurn = async (style: ApiStyle, omitSummary: boolean) => {
    const body =
      style === "responses"
        ? responsesRequestBody(input.model, input.turns, input.reasoning, {
            summary: !omitSummary,
          })
        : chatRequestBody(input.model, input.turns, input.reasoning)
    armIdle()
    const opened = await openBody({
      url: completionURL(base, style),
      headers: authHeaders(input.provider, true),
      body: JSON.stringify(body),
      signal: ctrl.signal,
      timeoutMs: idleMs,
      fetchImpl: input.fetchImpl,
      nativeStream: input.nativeStream,
    })
    clearTimeout(idleTimer)
    const iterator = opened.chunks[Symbol.asyncIterator]()
    if (opened.status < 200 || opened.status >= 300) {
      let raw = ""
      await readBody(iterator, async (chunk) => {
        raw += chunk
      }, ctrl, idleMs)
      throw new ModelCallError(
        redact(errorText(raw, opened.status), input.provider.apiKey),
        "http",
      )
    }
    await readCompletion(iterator, input.onDelta, ctrl, idleMs)
  }
  try {
    // A translator onto chat completions rejects a reasoning summary and an
    // image part with the same phrase. Dropping the summary keeps a text
    // turn on responses. An image part has to move to chat completions,
    // which is the shape that translator already accepts. Anything else is
    // the error the user sees. A responses endpoint that accepts the body
    // is not called twice.
    let style: ApiStyle = api
    let omitSummary = false
    for (let attempt = 0; attempt < 3; attempt++) {
      try {
        await postTurn(style, omitSummary)
        break
      } catch (err) {
        const kind =
          style === "responses" &&
          err instanceof ModelCallError &&
          err.kind === "http" &&
          !ctrl.signal.aborted &&
          !input.signal?.aborted
            ? rejectedField(err.message)
            : ""
        if (kind === "summary" && !omitSummary) {
          omitSummary = true
          continue
        }
        if (kind === "part") {
          style = "chat"
          continue
        }
        throw err
      }
    }
  } catch (err) {
    if (err instanceof ModelCallError) throw err
    if (ctrl.signal.aborted && ctrl.signal.reason === "timeout") {
      throw new ModelCallError("timeout", "timeout")
    }
    if (ctrl.signal.aborted || input.signal?.aborted) {
      throw new ModelCallError("abort", "abort")
    }
    throw new ModelCallError("network", "http")
  } finally {
    clearTimeout(idleTimer)
    input.signal?.removeEventListener("abort", stop)
  }
}

function rejectedField(message: string): "summary" | "part" | "" {
  const text = message.toLowerCase()
  if (!text.includes("no chat completions equivalent")) return ""
  if (text.includes("content part")) return "part"
  if (text.includes("summary")) return "summary"
  return ""
}

async function readCompletion(
  iterator: AsyncIterator<string>,
  onDelta: (piece: StreamPiece) => void,
  ctrl: AbortController,
  idleMs: number,
) {
  let pending = ""
  let text = ""
  let reasoning = ""
  let shown: StreamPiece = { text: "", reasoning: "" }
  let emitted = false
  const apply = async (piece: StreamPiece): Promise<boolean> => {
    const nextText = absorbChunk(text, piece.text)
    const nextReason = absorbChunk(reasoning, piece.reasoning)
    if (nextText === text && nextReason === reasoning) return false
    text = nextText
    reasoning = nextReason
    const next = present(text, reasoning, false)
    if (next.text === shown.text && next.reasoning === shown.reasoning) return false
    emitted = true
    // The thought lands before the answer, the same order a PC thread paints.
    const startedAnswer =
      !shown.text && Boolean(next.text) && Boolean(next.reasoning) && next.reasoning !== shown.reasoning
    if (startedAnswer) {
      onDelta({ text: "", reasoning: next.reasoning })
      await nextFrame()
      if (ctrl.signal.aborted) {
        shown = { text: "", reasoning: next.reasoning }
        return true
      }
    }
    shown = next
    onDelta(next)
    return true
  }
  const take = async (data: string): Promise<boolean> => {
    const piece = pieceFromData(data)
    if (piece === "done") return false
    if (piece) return apply(piece)
    // A stream that sends only the completed snapshot has no deltas.
    // Once tokens have arrived, that snapshot would paint them twice.
    if (emitted) return false
    let json: unknown
    try {
      json = JSON.parse(data)
    } catch {
      return false
    }
    const full = replyFromJSON(json)
    if (!full.text && !full.reasoning) return false
    return apply(full)
  }
  const consume = async (chunk: string) => {
    const taken = takeSSE(pending + normalizeNewlines(chunk))
    pending = taken.rest
    for (const ev of taken.events) {
      if (ctrl.signal.aborted) return
      if (await take(ev.data)) await nextFrame()
    }
  }
  await readBody(iterator, consume, ctrl, idleMs)
  // A JSON body is not an SSE event. Forcing a blank line onto it would
  // treat the first colon as a field name. "data:" inside the answer is
  // the same trap.
  const tail = looksLikeSSE(pending) ? takeSSE(pending + "\n\n") : { events: [], rest: pending }
  for (const ev of tail.events) {
    if (ctrl.signal.aborted) throw stopError(ctrl)
    if (await take(ev.data)) await nextFrame()
  }
  if (!emitted) {
    const raw = tail.rest.trim()
    if (raw) {
      let json: unknown
      try {
        json = JSON.parse(raw)
      } catch {
        throw new ModelCallError("parse", "parse")
      }
      const full = replyFromJSON(json)
      if (full.text || full.reasoning) await apply(full)
    }
  }
  const flushed = present(text, reasoning, true)
  if (flushed.text !== shown.text || flushed.reasoning !== shown.reasoning) {
    shown = flushed
    onDelta(flushed)
    await nextFrame()
  }
}

/** React commits on a task boundary. Events applied in one read would
 *  otherwise paint as the finished reply. rAF pauses in a backgrounded
 *  webview and would trip the idle clock. */
function nextFrame(): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, 0)
  })
}

function present(rawText: string, rawReason: string, flush: boolean): StreamPiece {
  const peeled = peelThought(rawText, flush)
  return { text: peeled.text, reasoning: mergeReason(rawReason, peeled.reasoning) }
}

function mergeReason(wire: string, tagged: string): string {
  if (!tagged) return wire
  if (!wire || wire.includes(tagged)) return wire || tagged
  if (tagged.includes(wire)) return tagged
  return wire + tagged
}

async function readBody(
  iterator: AsyncIterator<string>,
  onChunk: (chunk: string) => Promise<void>,
  ctrl: AbortController,
  idleMs: number,
) {
  try {
    for (;;) {
      if (ctrl.signal.aborted) throw stopError(ctrl)
      let timer: ReturnType<typeof setTimeout> | undefined
      const idle = new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
          ctrl.abort("timeout")
          reject(new ModelCallError("timeout", "timeout"))
        }, idleMs)
      })
      const reading = iterator.next().then(
        (value) => value,
        (err: unknown) => {
          // cancel() rejects the in-flight read. A real socket error must
          // not look like a finished reply.
          if (ctrl.signal.aborted) return { done: true as const, value: undefined }
          throw err
        },
      )
      const next = await Promise.race([reading, idle]).finally(() => clearTimeout(timer))
      if (next.done) break
      if (next.value) await onChunk(next.value)
    }
    if (ctrl.signal.aborted) throw stopError(ctrl)
  } finally {
    await iterator.return?.().catch(() => undefined)
  }
}

function stopError(ctrl: AbortController): ModelCallError {
  const timeout = ctrl.signal.reason === "timeout"
  return new ModelCallError(timeout ? "timeout" : "abort", timeout ? "timeout" : "abort")
}

function authHeaders(row: DirectProvider, json: boolean): Record<string, string> {
  const headers: Record<string, string> = {
    Accept: "application/json, text/event-stream",
  }
  if (json) {
    headers["Content-Type"] = "application/json"
    // A gzip body arrives as one blob. Identity keeps each token a chunk.
    headers["Accept-Encoding"] = "identity"
  }
  const key = row.apiKey.trim()
  if (key) headers.Authorization = "Bearer " + key
  return headers
}
