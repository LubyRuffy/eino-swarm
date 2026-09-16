import { normalizeSelectedText } from "@/lib/selection"

/** One snippet the user pulled out of the transcript to send with the next
 *  message. The composer shows these as annotations, not as textarea text,
 *  so the question stays editable on its own. */
export interface Quote {
  id: string
  text: string
}

/** Marker wrapped around quoted transcript text in the message that is
 *  actually sent. Stable on purpose: the user bubble and the model both
 *  see it, and tests assert on the marker rather than on whatever was
 *  selected. */
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

/** Prefixes the draft with each quote so the model sees what was selected
 *  without the composer dumping that text into the box. */
export function formatQuotedMessage(quotes: readonly string[], draft: string): string {
  const parts = quotes.map(normalizeSelectedText).filter(Boolean)
  const body = draft.trim()
  if (parts.length === 0) return body
  const block = parts.map((q) => `${SELECTED_TEXT_LABEL}:\n${q}`).join("\n\n")
  return body ? `${block}\n\n${body}` : block
}

function quoteId(n: number): string {
  return `q${n}-${Date.now().toString(36)}`
}
