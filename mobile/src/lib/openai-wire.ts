/** OpenAI-compatible request and stream shapes for a phone that calls the
 *  endpoint itself. Two wires: chat/completions and responses. An empty
 *  thinking level is omitted — a non-reasoning endpoint rejects the field. */

export type ApiStyle = "chat" | "responses"

export type WirePart =
  | { kind: "text"; text: string }
  | { kind: "image"; mediaType: string; dataUrl: string }
  | { kind: "file"; name: string; mediaType: string; dataUrl: string }

export type WireTurn = {
  role: "user" | "assistant"
  parts: WirePart[]
}

export type StreamPiece = { text: string; reasoning: string }

export class ModelCallError extends Error {
  constructor(
    message: string,
    readonly kind: "http" | "timeout" | "abort" | "parse",
  ) {
    super(message)
    this.name = "ModelCallError"
  }
}

const LEVELS = new Set(["low", "medium", "high"])

/** Same three levels as config.ReasoningEfforts. Anything else is the
 *  model's own default and must not be sent. */
export function normalizeReasoning(raw: string): string {
  const v = raw.trim().toLowerCase()
  return LEVELS.has(v) ? v : ""
}

export function normalizeApiStyle(raw: string): ApiStyle {
  return raw.trim().toLowerCase() === "responses" ? "responses" : "chat"
}

/** Only http(s). A pasted script or file URL must not become a request. */
export function httpBaseURL(raw: string): string | null {
  const text = raw.trim()
  if (!text) return null
  let url: URL
  try {
    url = new URL(text)
  } catch {
    return null
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") return null
  if (!url.hostname) return null
  return url.toString().replace(/\/+$/, "")
}

export function modelsURL(baseURL: string): string {
  return baseURL.replace(/\/+$/, "") + "/models"
}

export function completionURL(baseURL: string, api: ApiStyle): string {
  const base = baseURL.replace(/\/+$/, "")
  return base + (api === "responses" ? "/responses" : "/chat/completions")
}

export function chatRequestBody(
  model: string,
  turns: WireTurn[],
  reasoning: string,
): Record<string, unknown> {
  const body: Record<string, unknown> = {
    model,
    stream: true,
    messages: turns.map(chatMessage),
  }
  const effort = normalizeReasoning(reasoning)
  if (effort) body.reasoning_effort = effort
  return body
}

export function responsesRequestBody(
  model: string,
  turns: WireTurn[],
  reasoning: string,
): Record<string, unknown> {
  const body: Record<string, unknown> = {
    model,
    stream: true,
    input: turns.map(responsesMessage),
  }
  const effort = normalizeReasoning(reasoning)
  if (effort) body.reasoning = { effort }
  return body
}

function chatMessage(turn: WireTurn): Record<string, unknown> {
  const text = joinText(turn.parts)
  const rich = turn.parts.filter((part) => part.kind !== "text")
  if (rich.length === 0) return { role: turn.role, content: text }
  const content: Record<string, unknown>[] = []
  if (text) content.push({ type: "text", text })
  for (const part of rich) {
    if (part.kind === "image") {
      content.push({ type: "image_url", image_url: { url: part.dataUrl } })
    } else if (part.kind === "file") {
      content.push({
        type: "file",
        file: { filename: part.name, file_data: part.dataUrl },
      })
    }
  }
  return { role: turn.role, content }
}

function responsesMessage(turn: WireTurn): Record<string, unknown> {
  const textType = turn.role === "assistant" ? "output_text" : "input_text"
  const content: Record<string, unknown>[] = []
  for (const part of turn.parts) {
    if (part.kind === "text") {
      if (part.text) content.push({ type: textType, text: part.text })
    } else if (part.kind === "image") {
      content.push({ type: "input_image", image_url: part.dataUrl })
    } else {
      content.push({
        type: "input_file",
        filename: part.name,
        file_data: part.dataUrl,
      })
    }
  }
  if (content.length === 0) content.push({ type: textType, text: "" })
  return { role: turn.role, content }
}

function joinText(parts: WirePart[]): string {
  return parts
    .filter((part) => part.kind === "text")
    .map((part) => part.text)
    .filter(Boolean)
    .join("\n\n")
}

export type SSEEvent = { event: string; data: string }

export function normalizeNewlines(raw: string): string {
  return raw.replace(/\r\n/g, "\n").replace(/\r/g, "\n")
}

/** Events are split on a blank line. A trailing partial stays in rest so
 *  the next chunk can finish it. */
export function takeSSE(buffer: string): { events: SSEEvent[]; rest: string } {
  const events: SSEEvent[] = []
  let rest = buffer
  for (;;) {
    const cut = rest.indexOf("\n\n")
    if (cut < 0) break
    const raw = rest.slice(0, cut)
    rest = rest.slice(cut + 2)
    const ev = parseSSEBlock(raw)
    if (ev) events.push(ev)
  }
  return { events, rest }
}

function parseSSEBlock(raw: string): SSEEvent | null {
  let event = ""
  const data: string[] = []
  for (const line of raw.split("\n")) {
    if (!line || line.startsWith(":")) continue
    const idx = line.indexOf(":")
    const field = idx < 0 ? line : line.slice(0, idx)
    let value = idx < 0 ? "" : line.slice(idx + 1)
    if (value.startsWith(" ")) value = value.slice(1)
    if (field === "event") event = value
    else if (field === "data") data.push(value)
  }
  if (!event && data.length === 0) return null
  return { event, data: data.join("\n") }
}

/** A gateway that puts the whole buffer in every chunk must not paint the
 *  sentence again. A one-rune repeat is still a repeat; a sentence-sized
 *  echo is the snapshot. Same rule as signal.absorbChunk. */
const resentMinRunes = 12

export function absorbChunk(prev: string, chunk: string): string {
  if (!chunk) return prev
  if (!prev) return chunk
  if (chunk === prev && [...prev].length >= resentMinRunes) return prev
  if (chunk.length > prev.length && chunk.startsWith(prev)) return chunk
  return prev + chunk
}

/** A JSON reply can contain the letters "data:" inside the answer. Only a
 *  body that starts as an SSE frame is a stream. */
export function looksLikeSSE(raw: string): boolean {
  const line = raw.trimStart()
  return line.startsWith("data:") || line.startsWith("event:")
}

/** Delta events append. A terminal snapshot is ignored so the same sentence
 *  is not painted twice. `[DONE]` ends the stream. */
export function pieceFromData(data: string): StreamPiece | "done" | null {
  const trimmed = data.trim()
  if (!trimmed) return null
  if (trimmed === "[DONE]") return "done"
  let json: unknown
  try {
    json = JSON.parse(trimmed)
  } catch {
    return null
  }
  assertNoError(json)
  return deltaPiece(json)
}

export function replyFromJSON(json: unknown): StreamPiece {
  assertNoError(json)
  const root = asRecord(json)
  const direct = recordPiece(root)
  if (direct.text || direct.reasoning) return direct
  // response.completed wraps the finished reply one level down.
  return recordPiece(asRecord(root?.response))
}

function recordPiece(root: Record<string, unknown> | null): StreamPiece {
  const choice = firstRecord(root?.choices)
  const message = asRecord(choice?.message) ?? choice
  const fromChoice = textOf(message?.content) || textOf(choice?.text)
  const fromReason =
    stringOf(message?.reasoning_content) || stringOf(message?.reasoning)
  if (fromChoice || fromReason) return { text: fromChoice, reasoning: fromReason }
  const kind = stringOf(root?.type)
  if (kind.endsWith(".done")) {
    const text = stringOf(root?.text)
    if (text) return { text, reasoning: "" }
  }
  return outputPiece(root)
}

export function catalogNames(body: unknown): string[] {
  const data = asRecord(body)?.data
  if (!Array.isArray(data)) return []
  const out: string[] = []
  const seen = new Set<string>()
  for (const row of data) {
    const id = stringOf(asRecord(row)?.id).trim()
    if (!id || seen.has(id)) continue
    seen.add(id)
    out.push(id)
  }
  return out
}

export function errorText(raw: string, status: number): string {
  const trimmed = raw.trim()
  if (!trimmed) return String(status)
  try {
    const message = errorMessage(JSON.parse(trimmed))
    if (message) return clip(message)
  } catch {
    // The body was not JSON. The status line is still the failure.
  }
  return clip(trimmed.replace(/\s+/g, " "))
}

export function redact(message: string, secret: string): string {
  const key = secret.trim()
  if (!key) return message
  return message.split(key).join("•••")
}

function deltaPiece(json: unknown): StreamPiece | null {
  const root = asRecord(json)
  if (!root) return null
  const type = stringOf(root.type)
  if (type.endsWith(".done") || type === "response.completed") return null
  if (typeof root.delta === "string" && type.includes("reasoning")) {
    return { text: "", reasoning: root.delta }
  }
  if (typeof root.delta === "string" && (type.includes("output_text") || type === "")) {
    return { text: root.delta, reasoning: "" }
  }
  const choice = firstRecord(root.choices)
  const delta = asRecord(choice?.delta)
  if (!delta) return null
  const text = textOf(delta.content) || stringOf(delta.text)
  const reasoning =
    stringOf(delta.reasoning_content) || stringOf(delta.reasoning) || stringOf(delta.reasoning_text)
  if (!text && !reasoning) return null
  return { text, reasoning }
}

function outputPiece(root: Record<string, unknown> | null): StreamPiece {
  const output = root?.output
  if (!Array.isArray(output)) {
    const text = stringOf(root?.output_text)
    return { text, reasoning: "" }
  }
  let text = ""
  let reasoning = ""
  for (const item of output) {
    const row = asRecord(item)
    if (!row) continue
    if (row.type === "reasoning") {
      reasoning += summaryText(row.summary)
      continue
    }
    text += textOf(row.content)
  }
  return { text, reasoning }
}

function summaryText(summary: unknown): string {
  if (typeof summary === "string") return summary
  if (!Array.isArray(summary)) return ""
  return summary.map((item) => stringOf(asRecord(item)?.text)).join("")
}

function assertNoError(json: unknown) {
  const message = errorMessage(json)
  if (!message) return
  throw new ModelCallError(clip(message), "http")
}

function errorMessage(json: unknown): string {
  const root = asRecord(json)
  if (!root) return ""
  const type = stringOf(root.type)
  if (type.includes("failed")) {
    const nested = errorMessage(root.response) || errorMessage(root.error)
    if (nested) return nested
  }
  if (root.error == null && type !== "error") return ""
  if (typeof root.error === "string") return root.error
  const err = asRecord(root.error)
  return stringOf(err?.message) || stringOf(root.message)
}

function textOf(content: unknown): string {
  if (typeof content === "string") return content
  if (!Array.isArray(content)) return ""
  return content
    .map((part) => {
      const row = asRecord(part)
      return stringOf(row?.text) || stringOf(row?.refusal)
    })
    .join("")
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null
  return value as Record<string, unknown>
}

function firstRecord(value: unknown): Record<string, unknown> | null {
  if (!Array.isArray(value) || value.length === 0) return null
  return asRecord(value[0])
}

function stringOf(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function clip(message: string): string {
  const flat = message.replace(/\s+/g, " ").trim()
  return flat.length > 400 ? flat.slice(0, 400) : flat
}
