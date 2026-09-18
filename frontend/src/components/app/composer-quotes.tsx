import { Pencil, Quote as QuoteIcon, Trash2 } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import {
  SELECTED_TEXT_LABEL,
  editQuote,
  removeQuote,
  type Quote,
} from "@/lib/quote"
import { annotationLabelFor } from "@/lib/i18n"
import { useT } from "@/lib/use-t"

/** Chip in the composer: hover (or click) to read the quoted snippets,
 *  edit them, or drop them before they go out with the next send. */
export function ComposerQuotes({
  quotes,
  onChange,
}: {
  quotes: Quote[]
  onChange: (quotes: Quote[]) => void
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [editingId, setEditingId] = useState<string>()
  const [pos, setPos] = useState<{ left: number; bottom: number }>()
  const wrapRef = useRef<HTMLDivElement>(null)
  const closeTimer = useRef<number>(0)

  const cancelClose = () => {
    if (closeTimer.current) window.clearTimeout(closeTimer.current)
    closeTimer.current = 0
  }

  const scheduleClose = () => {
    if (editingId) return
    cancelClose()
    closeTimer.current = window.setTimeout(() => setOpen(false), 180)
  }

  useEffect(() => () => cancelClose(), [])

  useEffect(() => {
    if (quotes.length === 0) {
      setOpen(false)
      setEditingId(undefined)
    }
  }, [quotes.length])

  useEffect(() => {
    if (!open) {
      setPos(undefined)
      return
    }
    const update = () => {
      const el = wrapRef.current
      if (!el) return
      const r = el.getBoundingClientRect()
      setPos({ left: r.left, bottom: window.innerHeight - r.top + 8 })
    }
    update()
    window.addEventListener("resize", update)
    document.addEventListener("scroll", update, true)
    return () => {
      window.removeEventListener("resize", update)
      document.removeEventListener("scroll", update, true)
    }
  }, [open, quotes.length])

  useEffect(() => {
    if (!open) return
    const onDown = (event: PointerEvent) => {
      const target = event.target
      if (!(target instanceof Node)) return
      if (wrapRef.current?.contains(target)) return
      if (target instanceof Element && target.closest("[data-testid=quote-card]")) return
      setEditingId(undefined)
      setOpen(false)
    }
    document.addEventListener("pointerdown", onDown)
    return () => document.removeEventListener("pointerdown", onDown)
  }, [open])

  if (quotes.length === 0) return null

  const card =
    open && pos
      ? createPortal(
          <div
            data-testid="quote-card"
            role="dialog"
            aria-label={t("quote.dialog")}
            className="fixed z-50 w-80"
            style={{ left: pos.left, bottom: pos.bottom }}
            onMouseEnter={cancelClose}
            onMouseLeave={scheduleClose}
          >
            <div className="rounded-md border border-border bg-popover p-2 text-popover-foreground shadow-md">
              <ul className="flex flex-col gap-2">
                {quotes.map((q, i) => (
                  <QuoteRow
                    key={q.id}
                    index={i}
                    quote={q}
                    editing={editingId === q.id}
                    onEdit={() => setEditingId(q.id)}
                    onSave={(text) => {
                      onChange(editQuote(quotes, q.id, text))
                      setEditingId(undefined)
                    }}
                    onCancel={() => setEditingId(undefined)}
                    onRemove={() => {
                      onChange(removeQuote(quotes, q.id))
                      if (editingId === q.id) setEditingId(undefined)
                    }}
                  />
                ))}
              </ul>
            </div>
          </div>,
          document.body,
        )
      : null

  return (
    <div
      ref={wrapRef}
      className="relative px-3 pt-3"
      onMouseEnter={() => {
        cancelClose()
        setOpen(true)
      }}
      onMouseLeave={scheduleClose}
    >
      <Button
        type="button"
        variant="outline"
        size="sm"
        data-testid="quote-chip"
        aria-label={annotationLabelFor(quotes.length, t.locale)}
        aria-expanded={open}
        className="rounded-full"
        onClick={() => {
          cancelClose()
          setOpen(true)
        }}
      >
        <QuoteIcon />
        {annotationLabelFor(quotes.length, t.locale)}
      </Button>
      {card}
    </div>
  )
}

function QuoteRow({
  index,
  quote,
  editing,
  onEdit,
  onSave,
  onCancel,
  onRemove,
}: {
  index: number
  quote: Quote
  editing: boolean
  onEdit: () => void
  onSave: (text: string) => void
  onCancel: () => void
  onRemove: () => void
}) {
  const t = useT()
  const [draft, setDraft] = useState(quote.text)
  const areaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    setDraft(quote.text)
  }, [quote.text])

  useEffect(() => {
    if (editing) areaRef.current?.focus()
  }, [editing])

  const n = index + 1
  return (
    <li className="flex flex-col gap-1">
      <div className="flex items-start gap-1">
        <p className="min-w-0 flex-1 text-xs leading-5">
          <span className="text-muted-foreground">
            {n}. {SELECTED_TEXT_LABEL}:
          </span>
          {editing ? null : (
            <>
              {" "}
              <span className="whitespace-pre-wrap break-words text-foreground">
                {quote.text}
              </span>
            </>
          )}
        </p>
        {editing ? null : (
          <div className="flex shrink-0">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("quote.edit", { n })}
              onClick={onEdit}
            >
              <Pencil />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("quote.remove", { n })}
              onClick={onRemove}
            >
              <Trash2 />
            </Button>
          </div>
        )}
      </div>
      {editing ? (
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
      ) : null}
    </li>
  )
}
