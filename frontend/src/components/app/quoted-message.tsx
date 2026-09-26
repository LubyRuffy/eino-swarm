import { Pencil, Trash2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import { parseQuotedMessage } from "@/lib/quote"

/** Highlight chip. A long selection used to size the row to the raw
 *  string (`w-fit` + nowrap), which stretched the composer and the
 *  bubble. Three wrapped lines is the cap; the rest is an ellipsis.
 *  Edit still holds the whole passage. */
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
  const n = index + 1
  return (
    <div
      data-testid="quote-snippet"
      className={cn(
        "flex w-full min-w-0 max-w-full items-start gap-1 rounded-2xl border border-border px-2.5 py-1",
        detail
          ? "bg-popover text-popover-foreground shadow-md"
          : "bg-background",
      )}
    >
      <p
        data-testid="quote-snippet-text"
        className="min-w-0 flex-1 whitespace-pre-wrap break-words text-left text-xs leading-5 text-foreground line-clamp-3"
      >
        <span className="text-muted-foreground">
          {n}. {t("quote.selected")}:
        </span>{" "}
        {text}
      </p>
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
      className={cn("flex min-w-0 max-w-full flex-col gap-1.5", className)}
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
