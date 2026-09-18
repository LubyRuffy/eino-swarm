import { ScrollText, Sparkles } from "lucide-react"
import { useState } from "react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { localizeNotice } from "@/lib/i18n"
import { useT } from "@/lib/use-t"
import type { Block } from "@/lib/transcript"

/** Compact (and other chrome notices) stay a one-liner. When the reducer kept
 *  the briefing off the row, an icon opens it — dumping 8k runes into the
 *  chat would undo the whole point of the notice. Unmount the dialog when
 *  closed: a controlled Dialog left mounted can stick on data-state=closed. */
export function CompactNotice({ block }: { block: Block }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  if (block.quiet || !block.text) return null
  const briefing = block.detail?.trim() ?? ""
  return (
    <div
      data-testid="memory-notice"
      className="my-2 flex items-start gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground"
    >
      <Sparkles className="mt-0.5 size-3.5 shrink-0" />
      <p className="min-w-0 flex-1 stream-text whitespace-pre-wrap">
        {localizeNotice(block.text, t.locale)}
      </p>
      {briefing ? (
        <>
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            className="-mt-0.5 shrink-0 text-muted-foreground hover:text-foreground"
            onClick={() => setOpen(true)}
            aria-label={t("notice.briefing")}
            title={t("notice.briefing")}
            data-testid="compact-briefing-open"
          >
            <ScrollText />
          </Button>
          {open ? (
            <Dialog open onOpenChange={setOpen}>
              <DialogContent className="max-w-2xl">
                <DialogHeader>
                  <DialogTitle>{t("notice.briefingTitle")}</DialogTitle>
                  <DialogDescription>{t("notice.briefingHint")}</DialogDescription>
                </DialogHeader>
                <pre
                  data-testid="compact-briefing"
                  className="thin-scrollbar max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 text-sm leading-relaxed text-foreground"
                >
                  {briefing}
                </pre>
              </DialogContent>
            </Dialog>
          ) : null}
        </>
      ) : null}
    </div>
  )
}
