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
import { gatewayFetch } from "./gateway-fetch"
import { assertCloudWebToolsCurrent, cloudToolTraceText, currentCloudWebTools, executeCloudWebTool, validateCloudWebTool, type CloudToolEvent } from "./cloud-web-tools"
import { ToolContinuation, ToolRoundCollector } from "./openai-tool-wire"
import { watchWebAuthority } from "./web-execution"
import { loadGatewaySession, type GatewaySession } from "./gateway-account"
import { gatewayAuthorityCurrent, notifyGatewayRefusal, watchGatewayAuthority } from "./gateway-authority"

/** Discover is a button. It must not sit on the provider's five-minute idle clock. */
const discoverCapMs = 30_000

export type FetchLike = typeof fetch

export class ModelHttpError extends ModelCallError {
  constructor(message: string, readonly status: number) { super(message, "http") }
}

function modelGatewayAuthority(provider: DirectProvider): GatewaySession | null {
  if (provider.sourceKind !== "gateway") return null
  const session = loadGatewaySession()
  if (!session || !gatewayAuthorityCurrent(session) || provider.apiKey !== session.token || httpBaseURL(provider.baseURL) !== session.origin + "/v1") throw new ModelCallError("gateway_login_required", "http")
  return session
}

export async function discoverModels(
  row: DirectProvider,
  fetchImpl?: FetchLike,
): Promise<string[]> {
  const base = httpBaseURL(row.baseURL)
  if (!base) throw new ModelCallError("bad-url", "http")
  const authority = modelGatewayAuthority(row)
  const ctrl = new AbortController()
  const unwatch = authority ? watchGatewayAuthority(authority, () => ctrl.abort("stop")) : () => undefined
  const timer = setTimeout(() => ctrl.abort("timeout"), discoverCapMs)
  try {
    const managed = row.sourceKind === "gateway"
    const transport = fetchImpl ?? (managed ? gatewayFetch : fetch)
    const res = await transport(modelsURL(base), {
      method: "GET",
      headers: authHeaders(row, false),
      signal: ctrl.signal,
      ...(managed ? { redirect: "error" as const, credentials: "omit" as const, cache: "no-store" as const } : {}),
    })
    if (ctrl.signal.aborted) throw stopError(ctrl)
    if (authority && (res.status === 401 || res.status === 403)) {
      notifyGatewayRefusal(authority, res.status)
      await res.body?.cancel().catch(() => undefined)
      throw new ModelHttpError(res.status === 401 ? "gateway_login_required" : "gateway_access_denied", res.status)
    }
    const raw = await res.text()
    if (!res.ok) {
      if (authority) notifyGatewayRefusal(authority, res.status)
      throw new ModelHttpError(redact(errorText(raw, res.status), row.apiKey), res.status)
    }
    if (ctrl.signal.aborted) throw stopError(ctrl)
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
    if (ctrl.signal.aborted) throw stopError(ctrl)
    if (ctrl.signal.reason === "timeout") throw new ModelCallError("timeout", "timeout")
    throw new ModelCallError("network", "http")
  } finally {
    clearTimeout(timer)
    unwatch()
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
  onTool?: (event: CloudToolEvent) => void
}): Promise<void> {
  const base = httpBaseURL(input.provider.baseURL)
  if (!base) throw new ModelCallError("bad-url", "http")
  const api = input.provider.api
  const ctrl = new AbortController()
  const authority = modelGatewayAuthority(input.provider)
  const tools = currentCloudWebTools(input.fetchImpl)
  const continuation = tools ? new ToolContinuation() : null
  let seenToolFrames = false
  let sawToken = false
  let previousText = ""
  let previousReasoning = ""
  let outputBytes = 0
  const stop = () => {
    if (!ctrl.signal.aborted) ctrl.abort(input.signal?.reason ?? "stop")
  }
  input.signal?.addEventListener("abort", stop)
  if (input.signal?.aborted) stop()
  const unwatch = tools ? watchWebAuthority(() => {
    try { assertCloudWebToolsCurrent(tools) } catch { ctrl.abort("stop") }
  }) : () => undefined
  const accountAuthority = authority ?? tools?.session
  const unwatchAccount = accountAuthority ? watchGatewayAuthority(accountAuthority, () => ctrl.abort("stop")) : () => undefined
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
  const postTurn = async (style: ApiStyle, omitSummary: boolean, onToken: () => void) => {
    if (ctrl.signal.aborted) throw stopError(ctrl)
    const body =
      style === "responses"
        ? responsesRequestBody(input.model, input.turns, input.reasoning, {
            summary: !omitSummary,
          })
        : chatRequestBody(input.model, input.turns, input.reasoning)
    continuation?.request(body, style)
    const collector = tools ? new ToolRoundCollector(style) : undefined
    armIdle()
    const opened = await openBody({
      url: completionURL(base, style),
      headers: authHeaders(input.provider, true),
      body: JSON.stringify(body),
      signal: ctrl.signal,
      timeoutMs: idleMs,
      fetchImpl: input.fetchImpl,
      nativeStream: input.nativeStream,
      rejectRedirects: input.provider.sourceKind === "gateway",
    })
    if (ctrl.signal.aborted) { await opened.cancel(); throw stopError(ctrl) }
    clearTimeout(idleTimer)
    const iterator = opened.chunks[Symbol.asyncIterator]()
    if (authority && (opened.status === 401 || opened.status === 403)) {
      notifyGatewayRefusal(authority, opened.status)
      await opened.cancel()
      throw new ModelHttpError(opened.status === 401 ? "gateway_login_required" : "gateway_access_denied", opened.status)
    }
    if (opened.status < 200 || opened.status >= 300) {
      let raw = ""
      await readBody(iterator, async (chunk) => {
        raw += chunk
      }, ctrl, idleMs)
      if (authority) notifyGatewayRefusal(authority, opened.status)
      const message = tools && toolUnsupported(raw, opened.status) ? "cloud_tools_model_unsupported" : redact(errorText(raw, opened.status), input.provider.apiKey)
      throw new ModelHttpError(message, opened.status)
    }
    try {
      const piece = await readCompletion(iterator, (delta) => {
        onToken()
        input.onDelta({ text: previousText + delta.text, reasoning: previousReasoning + delta.reasoning })
      }, ctrl, idleMs, collector)
      return { piece, round: collector?.finish() }
    } finally { seenToolFrames ||= collector?.seen ?? false }
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
    for (let roundIndex = 0; roundIndex < 8; roundIndex++) {
      if (tools) assertCloudWebToolsCurrent(tools)
      let completed: Awaited<ReturnType<typeof postTurn>> | undefined
      for (let attempt = 0; attempt < 4; attempt++) {
        try {
          completed = await postTurn(style, omitSummary, () => {
            sawToken = true
          })
          break
        } catch (err) {
          const stopped = ctrl.signal.aborted || Boolean(input.signal?.aborted)
          if (stopped) throw err instanceof ModelCallError ? err : stopError(ctrl)
          const modelErr =
            err instanceof ModelCallError ? err : new ModelCallError("network", "http")
          const retry = style === "responses" && !sawToken && !seenToolFrames && !(continuation?.calls) && modelErr.kind !== "abort"
          const kind = retry && modelErr.kind === "http" ? rejectedField(modelErr.message) : ""
          if (kind === "summary" && !omitSummary) {
            omitSummary = true
            continue
          }
          if (kind === "part" || (retry && fallbackToChat(modelErr))) {
            style = "chat"
            continue
          }
          throw modelErr
        }
      }
      if (!completed) throw new ModelCallError("parse", "parse")
      const calls = completed.round?.calls ?? []
      if (!tools || !continuation || !completed.round || !calls.length) break
      if (roundIndex === 7 || continuation.calls + calls.length > 16) throw new ModelCallError("cloud_tools_limit", "parse")
      continuation.assertFresh(style, completed.round)
      calls.forEach(validateCloudWebTool)
      const outputs: string[] = []
      for (const call of calls) {
        const event = { id: `${roundIndex}:${call.id}`, callId: call.id, responseId: completed.round.responseID || undefined, name: call.name, args: call.arguments.slice(0, 4096), text: "" }
        input.onTool?.({ ...event, status: "running" })
        try {
          const output = await executeCloudWebTool(tools, call, ctrl.signal, idleMs)
          outputBytes += new TextEncoder().encode(output).length
          if (outputBytes > (512 << 10)) throw new ModelCallError("cloud_tools_output_limit", "parse")
          outputs.push(output)
          const preview = cloudToolTraceText(call.name, output)
          input.onTool?.({ ...event, status: "completed", text: preview.slice(0, 4096), truncated: preview.length > 4096 })
        } catch (error) {
          input.onTool?.({ ...event, status: ctrl.signal.aborted || error instanceof ModelCallError && error.kind === "abort" ? "cancelled" : "failed" })
          throw error
        }
      }
      assertCloudWebToolsCurrent(tools)
      continuation.append(style, completed.round, completed.piece, outputs)
      previousText += completed.piece.text
      previousReasoning += completed.piece.reasoning
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
    unwatch()
    unwatchAccount()
    input.signal?.removeEventListener("abort", stop)
  }
}

function toolUnsupported(raw: string, status: number): boolean {
  if (status !== 400 && status !== 422) return false
  try {
    const value = JSON.parse(raw) as { error?: { param?: unknown; code?: unknown; message?: unknown } }
    const error = value?.error
    if (!error || typeof error.message !== "string") return false
    const message = error.message.toLowerCase()
    return (error.param === "tools" && error.code === "unsupported_parameter") || /(?:does not support|doesn't support|not support|unsupported) (?:function calling|tool calling|tools|functions)|(?:function calling|tool calling|tools|functions) (?:is |are )?(?:not supported|unsupported)/.test(message)
  } catch { return false }
}

function fallbackToChat(err: ModelCallError): boolean {
  // A responses URL that never speaks is retried as chat completions.
  // A model error from an endpoint that accepted the body is not sent twice.
  if (err.kind === "parse" || err.message === "network") return true
  if (err.kind !== "http") return false
  const text = err.message.toLowerCase()
  return text === "404" || text === "405" || text === "501" || text.includes("not found")
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
  collector?: ToolRoundCollector,
) {
  let pending = ""
  let text = ""
  let reasoning = ""
  let shown: StreamPiece = { text: "", reasoning: "" }
  let emitted = false
  let ended = false
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
    try {
      collector?.accept(data)
      const piece = pieceFromData(data)
      if (piece === "done") { ended = true; return false }
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
    } finally { if (collector?.ended) ended = true }
  }
  const consume = async (chunk: string) => {
    const taken = takeSSE(pending + normalizeNewlines(chunk))
    pending = taken.rest
    for (const ev of taken.events) {
      if (ctrl.signal.aborted || ended) break
      if (await take(ev.data)) await nextFrame()
    }
    return !ended
  }
  await readBody(iterator, consume, ctrl, idleMs)
  // A JSON body is not an SSE event. Forcing a blank line onto it would
  // treat the first colon as a field name. "data:" inside the answer is
  // the same trap.
  const tail = ended ? { events: [], rest: "" } : looksLikeSSE(pending) ? takeSSE(pending + "\n\n") : { events: [], rest: pending }
  for (const ev of tail.events) {
    if (ended) break
    if (ctrl.signal.aborted) throw stopError(ctrl)
    if (await take(ev.data)) await nextFrame()
  }
  if (!emitted) {
    const raw = tail.rest.trim()
    if (raw) {
      collector?.accept(raw)
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
  return shown
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
  onChunk: (chunk: string) => Promise<void | boolean>,
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
      if (next.value && await onChunk(next.value) === false) break
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
