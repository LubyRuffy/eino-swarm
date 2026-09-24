/** The phone and desktop use the same tagged quote format on the wire. */
export const SELECTED_TEXT_TAG = "selected_text"
export const USER_REQUEST_TAG = "user_request"
export const SELECTED_TEXT_LABEL = "Selected text"

export interface QuotedPayload {
  quotes: string[]
  body: string
}

export function normalizeSelectedText(raw: string): string {
  return raw.replace(/\u00a0/g, " ").replace(/\r\n?/g, "\n").trim()
}

export function formatQuotedMessage(quotes: readonly string[], draft: string): string {
  const selected = quotes.map(normalizeSelectedText).filter(Boolean)
  const body = draft.trim()
  if (selected.length === 0) return body
  const blocks = selected.map((text) =>
    `<${SELECTED_TEXT_TAG}>\n${text.replaceAll(`</${SELECTED_TEXT_TAG}>`, `</ ${SELECTED_TEXT_TAG}>`)}\n</${SELECTED_TEXT_TAG}>`,
  )
  if (!body) return blocks.join("\n\n")
  return `${blocks.join("\n\n")}\n\n<${USER_REQUEST_TAG}>\n${body.replaceAll(`</${USER_REQUEST_TAG}>`, `</ ${USER_REQUEST_TAG}>`)}\n</${USER_REQUEST_TAG}>`
}

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

function collectTag(text: string, tag: string): string[] {
  const re = new RegExp(`<${tag}>\\n?([\\s\\S]*?)\\n?</${tag}>`, "g")
  const out: string[] = []
  for (const m of text.matchAll(re)) {
    out.push((m[1] ?? "").replaceAll(`</ ${tag}>`, `</${tag}>`))
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
