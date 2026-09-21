import { Quote as QuoteIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { QuoteSnippet } from "@/components/app/quoted-message"
import { badgeVariants } from "@/components/ui/badge"
import { Textarea } from "@/components/ui/textarea"
import { editQuote, removeQuote, type Quote } from "@/lib/quote"
import { annotationLabelFor } from "@/lib/i18n"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"

/** Count chip at rest. Hover (or focus / a tap) opens the highlight so
 *  it can be read, edited or dropped — not a second bubble in the box. */
export function ComposerQuotes({
  quotes,
  onChange,
}: {
  quotes: Quote[]
  onChange: (quotes: Quote[]) => void
}) {
  const t = useT()
  const rootRef = useRef<HTMLDivElement>(null)
  const [editingId, setEditingId] = useState<string>()
  const [hovered, setHovered] = useState(false)
  const [pinned, setPinned] = useState(false)

  useEffect(() => {
    if (quotes.length === 0) {
      setEditingId(undefined)
      setHovered(false)
      setPinned(false)
    }
  }, [quotes.length])

  if (quotes.length === 0) return null

  const showDetails = hovered || pinned || Boolean(editingId)
  const label = annotationLabelFor(quotes.length, t.locale)

  const closeIfLeft = (next: EventTarget | null) => {
    if (!rootRef.current?.contains(next as Node | null)) setHovered(false)
  }

  return (
    <div
      ref={rootRef}
      className="relative"
      data-testid="composer-quotes"
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      {showDetails ? (
        <div
          id="composer-quote-details"
          data-testid="quote-details"
          className="flex flex-col gap-1.5 px-3 pt-3"
        >
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
                    detail
                    index={i}
                    text={q.text}
                    onEdit={() => setEditingId(q.id)}
                    onRemove={() => onChange(removeQuote(quotes, q.id))}
                  />
                )}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      <div className={showDetails ? "px-3 pt-1.5" : "px-3 pt-3"}>
        <button
          type="button"
          data-testid="quote-chip"
          aria-label={label}
          aria-expanded={showDetails}
          aria-controls={showDetails ? "composer-quote-details" : undefined}
          className={cn(
            badgeVariants({ variant: "outline" }),
            "rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          )}
          onClick={() => setPinned((v) => !v)}
          onFocus={() => setHovered(true)}
          onBlur={(e) => closeIfLeft(e.relatedTarget)}
        >
          <QuoteIcon className="size-3" />
          {label}
        </button>
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
      className="min-h-[4.5rem] bg-background px-2 py-1.5 text-xs"
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
