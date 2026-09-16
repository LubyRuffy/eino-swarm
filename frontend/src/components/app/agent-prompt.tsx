import { ScrollText } from "lucide-react"
import { useState } from "react"

import { CopyButton } from "@/components/app/transcript"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { useT } from "@/lib/use-t"

/** Opens the system prompt this worker was started with. The chrome has no
 *  room for the body; a dialog is the only place the whole instruction fits.
 *  Unmount when closed: a controlled Dialog left mounted can stick on
 *  data-state=closed at full opacity through the exit animation. */
export function AgentPromptButton({ instruction }: { instruction: string }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        className="shrink-0"
        onClick={() => setOpen(true)}
        aria-label={t("panel.prompt")}
        title={t("panel.prompt")}
      >
        <ScrollText />
      </Button>
      {open ? (
        <Dialog open onOpenChange={setOpen}>
          <DialogContent className="max-w-2xl">
            <DialogHeader>
              <DialogTitle>{t("panel.promptTitle")}</DialogTitle>
              <DialogDescription>{t("panel.promptHint")}</DialogDescription>
            </DialogHeader>
            <div className="flex justify-end">
              <CopyButton text={instruction} label={t("panel.copy")} />
            </div>
            <pre
              data-testid="agent-prompt"
              className="thin-scrollbar max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 font-mono text-xs leading-relaxed text-foreground"
            >
              {instruction}
            </pre>
          </DialogContent>
        </Dialog>
      ) : null}
    </>
  )
}
