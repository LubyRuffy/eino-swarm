import { Quote as QuoteIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { QuoteSnippet } from "@/components/app/quoted-message"
import { Badge } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { editQuote, removeQuote, type Quote } from "@/lib/quote"
import { annotationLabelFor } from "@/lib/i18n"
import { useT } from "@/lib/use-t"

/** Chips in the composer: one truncated line per highlight, edit or drop
 *  before they go out with the next send. The count badge is the summary
 *  Cursor/Codex put next to the box; the chips are what was selected. */
export function ComposerQuotes({
  quotes,
  onChange,
}: {
  quotes: Quote[]
  onChange: (quotes: Quote[]) => void
}) {
  const t = useT()
  const [editingId, setEditingId] = useState<string>()

  useEffect(() => {
    if (quotes.length === 0) setEditingId(undefined)
  }, [quotes.length])

  if (quotes.length === 0) return null

  return (
    <div className="flex flex-col gap-1.5 px-3 pt-3" data-testid="composer-quotes">
      <ul className="flex flex-col gap-1.5">
        {quotes.map((q, i) => (
          <li key={q.id}>
            {editingId === q.id ? (
              <QuoteEditor
                index={i}
                quote={q}
                onSave={(text) => {
                  onChange(editQuote(quotes, q.id, text))
                  setEditingId(undefined)
                }}
                onCancel={() => setEditingId(undefined)}
              />
            ) : (
              <QuoteSnippet
                index={i}
                text={q.text}
                onEdit={() => setEditingId(q.id)}
                onRemove={() => onChange(removeQuote(quotes, q.id))}
              />
            )}
          </li>
        ))}
      </ul>
      <div>
        <Badge
          variant="outline"
          data-testid="quote-chip"
          aria-label={annotationLabelFor(quotes.length, t.locale)}
          className="rounded-full"
        >
          <QuoteIcon className="size-3" />
          {annotationLabelFor(quotes.length, t.locale)}
        </Badge>
      </div>
    </div>
  )
}

function QuoteEditor({
  index,
  quote,
  onSave,
  onCancel,
}: {
  index: number
  quote: Quote
  onSave: (text: string) => void
  onCancel: () => void
}) {
  const t = useT()
  const [draft, setDraft] = useState(quote.text)
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const n = index + 1

  useEffect(() => {
    setDraft(quote.text)
  }, [quote.text])

  useEffect(() => {
    areaRef.current?.focus()
  }, [])

  return (
    <Textarea
      ref={areaRef}
      aria-label={t("quote.edit", { n })}
      value={draft}
      rows={3}
      className="min-h-[4.5rem] px-2 py-1.5 text-xs"
      onChange={(e) => setDraft(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault()
          setDraft(quote.text)
          onCancel()
        } else if (e.key === "Enter" && !e.shiftKey) {
          e.preventDefault()
          onSave(draft)
        }
      }}
      onBlur={() => onSave(draft)}
    />
  )
}
