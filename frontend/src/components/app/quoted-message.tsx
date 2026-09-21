import { Pencil, Trash2 } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import { parseQuotedMessage } from "@/lib/quote"

/** Transcript chip: one truncated line until opened. Composer hover
 *  passes `detail` so the full highlight is the hover panel, not a
 *  second bubble sitting in the box. */
export function QuoteSnippet({
  index,
  text,
  onEdit,
  onRemove,
  detail = false,
}: {
  index: number
  text: string
  onEdit?: () => void
  onRemove?: () => void
  detail?: boolean
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const expanded = detail || open
  const n = index + 1
  const body = (
    <>
      <span className="text-muted-foreground">
        {n}. {t("quote.selected")}:
      </span>{" "}
      <span
        className={
          expanded
            ? "whitespace-pre-wrap break-words text-foreground"
            : "inline-block max-w-full truncate align-bottom text-foreground"
        }
      >
        {text}
      </span>
    </>
  )
  return (
    <div
      data-testid="quote-snippet"
      className={cn(
        "flex w-fit max-w-full items-start gap-1 border border-border px-2.5 py-1",
        detail
          ? "bg-popover text-popover-foreground shadow-md"
          : "bg-background",
        expanded ? "rounded-2xl" : "rounded-full",
      )}
    >
      {detail ? (
        <div className="min-w-0 flex-1 text-left text-xs leading-5">{body}</div>
      ) : (
        <button
          type="button"
          className="min-w-0 flex-1 text-left text-xs leading-5"
          aria-expanded={open}
          aria-label={`${n}. ${t("quote.selected")}`}
          onClick={() => setOpen((v) => !v)}
        >
          {body}
        </button>
      )}
      {onEdit || onRemove ? (
        <div className="flex shrink-0">
          {onEdit ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-2xs"
              aria-label={t("quote.edit", { n })}
              onClick={(e) => {
                e.stopPropagation()
                onEdit()
              }}
            >
              <Pencil />
            </Button>
          ) : null}
          {onRemove ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-2xs"
              aria-label={t("quote.remove", { n })}
              onClick={(e) => {
                e.stopPropagation()
                onRemove()
              }}
            >
              <Trash2 />
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

/** User and steer bubbles: chips for highlights, then the instruction.
 *  Wire tags stay out of the DOM so the model protocol is not the UI. */
export function QuotedMessageBody({
  text,
  className,
}: {
  text: string
  className?: string
}) {
  const parsed = parseQuotedMessage(text)
  if (parsed.quotes.length === 0) {
    return (
      <p className={cn("stream-text whitespace-pre-wrap", className)}>{text}</p>
    )
  }
  return (
    <div
      data-testid="quoted-message"
      className={cn("flex flex-col gap-1.5", className)}
    >
      {parsed.quotes.map((q, i) => (
        <QuoteSnippet key={`${i}-${q.slice(0, 24)}`} index={i} text={q} />
      ))}
      {parsed.body ? (
        <p className="stream-text whitespace-pre-wrap">{parsed.body}</p>
      ) : null}
    </div>
  )
}
