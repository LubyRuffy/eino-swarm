import { ChevronRight, ScrollText, Sparkles } from "lucide-react"
import { useId, useState } from "react"

import { ReviewStatus } from "@/components/app/review-status"
import { Button } from "@/components/ui/button"
import { localizeNotice } from "@/lib/i18n"
import { useT, type Translate } from "@/lib/use-t"
import type { Block } from "@/lib/transcript"

function compactLabel(text: string, t: Translate): string {
  const tokens = /^Context compressed \((\d+) → (\d+) tokens\)\. The transcript is unchanged\.$/.exec(text)
  if (tokens) return t("notice.compactStatusTokens", { before: tokens[1], after: tokens[2] })
  if (text === "Earlier turns were folded into a briefing. The transcript is unchanged." ||
      text === "Context compressed. The transcript is unchanged.") {
    return t("notice.compactStatus")
  }
  return localizeNotice(text, t.locale)
}

/** Compression is secondary chrome; the complete briefing stays available
 *  inline. Other notice kinds, including failed compression, keep their card. */
export function CompactNotice({ block }: { block: Block }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const briefingId = useId()
  if (block.quiet || !block.text) return null
  if (block.review) return <ReviewStatus outcome={block.review} turnId={block.turnId} />
  const briefing = block.detail ?? ""
  const compacted = block.compacted === true
  const Icon = compacted ? ScrollText : Sparkles
  return (
    <div
      data-testid="memory-notice"
      className={compacted
        ? "my-1 text-xs text-muted-foreground"
        : "my-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground"}
    >
      <div className="flex flex-wrap items-start gap-x-2 gap-y-1">
        <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0" />
        <p className="min-w-0 flex-1 stream-text whitespace-pre-wrap leading-5">
          {compacted ? compactLabel(block.text, t) : localizeNotice(block.text, t.locale)}
        </p>
        {briefing.trim() ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-auto shrink-0 gap-1 px-1 py-0 text-xs font-normal leading-5 text-muted-foreground hover:text-foreground [&_svg]:size-3"
            onClick={() => setOpen((previous) => !previous)}
            aria-expanded={open}
            aria-controls={briefingId}
            data-testid="compact-briefing-open"
          >
            <ChevronRight aria-hidden className={open ? "rotate-90" : undefined} />
            {t(open ? "notice.briefingHide" : "notice.briefing")}
          </Button>
        ) : null}
      </div>
      {briefing.trim() ? (
        <div
          id={briefingId}
          hidden={!open}
          role="region"
          aria-label={t("notice.briefingTitle")}
        >
          {open ? (
            <pre
              data-testid="compact-briefing"
              className="mt-2 min-w-0 whitespace-pre-wrap break-words pl-5 text-sm leading-relaxed text-foreground"
            >
              {briefing}
            </pre>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
