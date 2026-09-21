import { normalizeSelectedText } from "@/lib/selection"

/** One snippet the user pulled out of the transcript to send with the next
 *  message. The composer shows a count chip at rest (hover to read / edit /
 *  drop), not as textarea text, so the question stays editable on its own. */
export interface Quote {
  id: string
  text: string
}

export interface QuotedPayload {
  quotes: string[]
  body: string
}

/** Wire tags on a quoted send. Stable on purpose: the model, the user
 *  bubble parser, and tests all key off these rather than on whatever was
 *  selected. UI copy is localized separately. */
export const SELECTED_TEXT_TAG = "selected_text"
export const USER_REQUEST_TAG = "user_request"

/** Legacy prefix from before the tagged wire format. Replay of old
 *  conversations still has to split on it. */
export const SELECTED_TEXT_LABEL = "Selected text"

export function annotationLabel(count: number): string {
  if (count <= 0) return ""
  return count === 1 ? "1 annotation" : `${count} annotations`
}

export function appendQuote(quotes: Quote[], text: string, id?: string): Quote[] {
  const normalized = normalizeSelectedText(text)
  if (!normalized) return quotes
  return [...quotes, { id: id ?? quoteId(quotes.length), text: normalized }]
}

export function editQuote(quotes: Quote[], id: string, text: string): Quote[] {
  const normalized = normalizeSelectedText(text)
  if (!normalized) return quotes.filter((q) => q.id !== id)
  return quotes.map((q) => (q.id === id ? { ...q, text: normalized } : q))
}

export function removeQuote(quotes: Quote[], id: string): Quote[] {
  return quotes.filter((q) => q.id !== id)
}

/** Prefixes each highlight as <selected_text> and the draft as
 *  <user_request> so the model can tell a quote from the instruction.
 *  An empty box still sends the quotes alone. */
export function formatQuotedMessage(quotes: readonly string[], draft: string): string {
  const parts = quotes.map(normalizeSelectedText).filter(Boolean)
  const body = draft.trim()
  if (parts.length === 0) return body
  const blocks = parts.map((q) => wrapTag(SELECTED_TEXT_TAG, q))
  if (!body) return blocks.join("\n\n")
  return `${blocks.join("\n\n")}\n\n${wrapTag(USER_REQUEST_TAG, body)}`
}

/** Split a stored user/steer payload into highlights and the instruction.
 *  Tagged messages round-trip. The old `Selected text:` prefix is best-effort
 *  so a reloaded conversation does not dump the wrapper into the bubble. */
export function parseQuotedMessage(text: string): QuotedPayload {
  const raw = text ?? ""
  if (!raw.trim()) return { quotes: [], body: "" }
  if (raw.includes(`<${SELECTED_TEXT_TAG}>`) || raw.includes(`<${USER_REQUEST_TAG}>`)) {
    return parseTagged(raw)
  }
  if (raw.startsWith(`${SELECTED_TEXT_LABEL}:`)) {
    return parseLegacy(raw)
  }
  return { quotes: [], body: raw }
}

/** What a label or jump tick should show: the human's request, not the
 *  wire tags. Quote-only sends fall back to the highlight. */
export function plainUserText(text: string): string {
  const parsed = parseQuotedMessage(text)
  if (parsed.quotes.length === 0) return text ?? ""
  if (parsed.body.trim()) return parsed.body
  return parsed.quotes.join(" ")
}

/** What Copy message puts on the clipboard: chips plus the instruction,
 *  never the `<selected_text>` wrappers the model sees. */
export function displayQuotedText(text: string): string {
  const parsed = parseQuotedMessage(text)
  if (parsed.quotes.length === 0) return text ?? ""
  const parts = [...parsed.quotes]
  if (parsed.body.trim()) parts.push(parsed.body)
  return parts.join("\n\n")
}

function parseTagged(text: string): QuotedPayload {
  const quotes = collectTag(text, SELECTED_TEXT_TAG)
  const requests = collectTag(text, USER_REQUEST_TAG)
  if (requests.length > 0) {
    return { quotes, body: requests.join("\n\n") }
  }
  const body = stripTags(text, [SELECTED_TEXT_TAG, USER_REQUEST_TAG]).trim()
  return { quotes, body }
}

function parseLegacy(text: string): QuotedPayload {
  const prefix = `${SELECTED_TEXT_LABEL}:`
  const rest = text.startsWith(`${prefix}\n`)
    ? text.slice(prefix.length + 1)
    : text.slice(prefix.length).replace(/^\n/, "")
  const chunks = rest.split(`\n\n${prefix}\n`)
  const quotes: string[] = []
  let body = ""
  chunks.forEach((chunk, i) => {
    if (i === chunks.length - 1) {
      const idx = chunk.indexOf("\n\n")
      if (idx >= 0) {
        quotes.push(chunk.slice(0, idx))
        body = chunk.slice(idx + 2)
        return
      }
    }
    quotes.push(chunk)
  })
  return { quotes, body }
}

function wrapTag(tag: string, body: string): string {
  return `<${tag}>\n${escapeClose(tag, body)}\n</${tag}>`
}

function escapeClose(tag: string, body: string): string {
  return body.replaceAll(`</${tag}>`, `</ ${tag}>`)
}

function unescapeClose(tag: string, body: string): string {
  return body.replaceAll(`</ ${tag}>`, `</${tag}>`)
}

function collectTag(text: string, tag: string): string[] {
  const re = new RegExp(`<${tag}>\\n?([\\s\\S]*?)\\n?</${tag}>`, "g")
  const out: string[] = []
  for (const m of text.matchAll(re)) {
    out.push(unescapeClose(tag, m[1] ?? ""))
  }
  return out
}

function stripTags(text: string, tags: string[]): string {
  let next = text
  for (const tag of tags) {
    next = next.replace(new RegExp(`<${tag}>\\n?[\\s\\S]*?\\n?</${tag}>`, "g"), "")
  }
  return next
}

function quoteId(n: number): string {
  return `q${n}-${Date.now().toString(36)}`
}
