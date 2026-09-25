import { useEffect, useState } from "react"

import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { api } from "@/lib/api"
import type { ClientTranscript } from "@/lib/local-clients"
import { useT } from "@/lib/use-t"

/** Read-only. No composer: the foreign session is not a zwai turn. */
export function ClientTranscriptDialog({
  id,
  onClose,
}: {
  id: string | null
  onClose: () => void
}) {
  const t = useT()
  const [doc, setDoc] = useState<ClientTranscript | null>(null)
  useEffect(() => {
    if (!id) {
      setDoc(null)
      return
    }
    let stop = false
    api
      .clientTask(id)
      .then((next) => {
        if (!stop) setDoc(next)
      })
      .catch(() => {
        if (!stop) setDoc(null)
      })
    return () => {
      stop = true
    }
  }, [id])

  return (
    <Dialog open={id != null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[80vh] max-w-2xl overflow-hidden">
        <DialogHeader>
          <DialogTitle>{doc?.title || t("sidebar.clients")}</DialogTitle>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">{t("sidebar.clientReadOnly")}</p>
        <div className="max-h-[60vh] space-y-2 overflow-y-auto" data-testid="client-transcript">
          {(doc?.entries ?? []).map((entry, i) => (
            <p key={`${entry.role}-${i}`} className="text-sm">
              <span className="text-muted-foreground">{entry.role}</span> {entry.text}
            </p>
          ))}
          {doc?.truncated ? (
            <p className="text-xs text-muted-foreground">{t("sidebar.clientTruncated")}</p>
          ) : null}
        </div>
      </DialogContent>
    </Dialog>
  )
}
