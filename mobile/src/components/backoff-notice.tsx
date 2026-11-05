import { useId, useState } from "react"
import { ChevronRight } from "lucide-react"

import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import type { CompactBlock } from "@/lib/transcript"
import { scheduleAnswerBackoffReason } from "../../../frontend/src/lib/transcript-backoff"

export function BackoffNotice({ block, text }: { block: CompactBlock; text: string }) {
  const [open, setOpen] = useState(false)
  const id = useId()
  const context = block.backoff
  const reason = context && scheduleAnswerBackoffReason(context)
  return (
    <div data-testid="backoff-notice" className="min-w-0 text-[11px] text-muted-foreground">
      <p>{text}</p>
      <Button type="button" variant="ghost" className="h-auto gap-1 px-0 py-1 text-[11px]"
        aria-expanded={open} aria-controls={id} onClick={() => setOpen((value) => !value)}>
        <ChevronRight aria-hidden className={open ? "size-3 rotate-90" : "size-3"} />
        {t(open ? "backoff.hide" : "backoff.details")}
      </Button>
      <div id={id} hidden={!open} role="region" aria-label={t("backoff.details")} className="space-y-1 leading-relaxed">
        {context?.attempt ? <p>{t("backoff.attempt", { count: context.attempt })}</p> : null}
        {reason ? <p>{t(reason)}</p> : null}
      </div>
    </div>
  )
}
