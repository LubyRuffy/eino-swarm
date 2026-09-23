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
  pieceFromData,
  redact,
  replyFromJSON,
  responsesRequestBody,
  takeSSE,
  type StreamPiece,
  type WireTurn,
} from "./openai-wire"
import type { DirectProvider } from "./direct-provider"
import { defaultTimeoutSeconds } from "./direct-provider"

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
  onDelta: (piece: StreamPiece) => void
}): Promise<void> {
  const base = httpBaseURL(input.provider.baseURL)
  if (!base) throw new ModelCallError("bad-url", "http")
  const api = input.provider.api
  const body =
    api === "responses"
      ? responsesRequestBody(input.model, input.turns, input.reasoning)
      : chatRequestBody(input.model, input.turns, input.reasoning)
  const ctrl = new AbortController()
  const stop = () => {
    if (!ctrl.signal.aborted) ctrl.abort(input.signal?.reason ?? "stop")
  }
  input.signal?.addEventListener("abort", stop)
  const seconds =
    input.provider.timeoutSeconds > 0 ? input.provider.timeoutSeconds : defaultTimeoutSeconds
  const idleMs = Math.max(1, Math.round(seconds * 1000))
  const fetchImpl = input.fetchImpl ?? fetch
  // Idle covers the wait for headers too. A socket that never answers
  // used to sit in pending until the OS gave up.
  let idleTimer: ReturnType<typeof setTimeout> | undefined
  const armIdle = () => {
    clearTimeout(idleTimer)
    idleTimer = setTimeout(() => {
      if (!ctrl.signal.aborted) ctrl.abort("timeout")
    }, idleMs)
  }
  armIdle()
  try {
    const res = await fetchImpl(completionURL(base, api), {
      method: "POST",
      headers: authHeaders(input.provider, true),
      body: JSON.stringify(body),
      signal: ctrl.signal,
    })
    clearTimeout(idleTimer)
    if (!res.ok) {
      const raw = await res.text()
      throw new ModelCallError(redact(errorText(raw, res.status), input.provider.apiKey), "http")
    }
    await readCompletion(res, input.onDelta, ctrl, idleMs)
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

async function readCompletion(
  res: Response,
  onDelta: (piece: StreamPiece) => void,
  ctrl: AbortController,
  idleMs: number,
) {
  let pending = ""
  let text = ""
  let reasoning = ""
  let emitted = false
  const apply = (piece: StreamPiece) => {
    const nextText = absorbChunk(text, piece.text)
    const nextReason = absorbChunk(reasoning, piece.reasoning)
    if (nextText === text && nextReason === reasoning) return
    text = nextText
    reasoning = nextReason
    emitted = true
    onDelta({ text, reasoning })
  }
  const take = (data: string) => {
    const piece = pieceFromData(data)
    if (piece === "done") return
    if (piece) {
      apply(piece)
      return
    }
    // A stream that sends only the completed snapshot has no deltas.
    // Once tokens have arrived, that snapshot would paint them twice.
    if (emitted) return
    let json: unknown
    try {
      json = JSON.parse(data)
    } catch {
      return
    }
    const full = replyFromJSON(json)
    if (full.text || full.reasoning) apply(full)
  }
  const consume = (chunk: string) => {
    const taken = takeSSE(pending + normalizeNewlines(chunk))
    pending = taken.rest
    for (const ev of taken.events) take(ev.data)
  }
  await readBody(res, consume, ctrl, idleMs)
  // A JSON body is not an SSE event. Forcing a blank line onto it would
  // treat the first colon as a field name. "data:" inside the answer is
  // the same trap.
  const tail = looksLikeSSE(pending) ? takeSSE(pending + "\n\n") : { events: [], rest: pending }
  for (const ev of tail.events) take(ev.data)
  if (emitted) return
  const raw = tail.rest.trim()
  if (!raw) return
  let json: unknown
  try {
    json = JSON.parse(raw)
  } catch {
    throw new ModelCallError("parse", "parse")
  }
  const full = replyFromJSON(json)
  if (full.text || full.reasoning) onDelta(full)
}

async function readBody(
  res: Response,
  onChunk: (chunk: string) => void,
  ctrl: AbortController,
  idleMs: number,
) {
  const reader = res.body?.getReader()
  if (!reader) {
    onChunk(await res.text())
    return
  }
  const dec = new TextDecoder()
  const onAbort = () => {
    void reader.cancel()
  }
  ctrl.signal.addEventListener("abort", onAbort)
  try {
    for (;;) {
      if (ctrl.signal.aborted) {
        throw new ModelCallError(ctrl.signal.reason === "timeout" ? "timeout" : "abort", ctrl.signal.reason === "timeout" ? "timeout" : "abort")
      }
      let timer: ReturnType<typeof setTimeout> | undefined
      const idle = new Promise<never>((_, reject) => {
        timer = setTimeout(() => {
          ctrl.abort("timeout")
          reject(new ModelCallError("timeout", "timeout"))
        }, idleMs)
      })
      const reading = reader.read().then(
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
      onChunk(dec.decode(next.value, { stream: true }))
    }
    if (ctrl.signal.aborted) {
      throw new ModelCallError(
        ctrl.signal.reason === "timeout" ? "timeout" : "abort",
        ctrl.signal.reason === "timeout" ? "timeout" : "abort",
      )
    }
    const rest = dec.decode()
    if (rest) onChunk(rest)
  } finally {
    ctrl.signal.removeEventListener("abort", onAbort)
    reader.releaseLock()
  }
}

function authHeaders(row: DirectProvider, json: boolean): Record<string, string> {
  const headers: Record<string, string> = {
    Accept: "application/json, text/event-stream",
  }
  if (json) headers["Content-Type"] = "application/json"
  const key = row.apiKey.trim()
  if (key) headers.Authorization = "Bearer " + key
  return headers
}
