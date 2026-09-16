import { Loader2, Paperclip } from "lucide-react"

import { useT } from "@/lib/use-t"

/** Covers the composer while a file drag is over it. Busy is the beat
 *  between drop and the chips/thumbs landing — without it the box looks dead. */
export function ComposerDropOverlay({ busy }: { busy: boolean }) {
  const t = useT()
  return (
    <div
      data-testid="composer-drop-overlay"
      role="status"
      aria-live="polite"
      aria-busy={busy || undefined}
      className="pointer-events-none absolute inset-0 z-20 flex flex-col items-center justify-center gap-2 rounded-3xl border-2 border-dashed border-ring bg-card text-sm text-muted-foreground"
    >
      {busy ? (
        <Loader2 className="size-5 animate-spin" aria-hidden />
      ) : (
        <Paperclip className="size-5" aria-hidden />
      )}
      <span>{busy ? t("composer.adding") : t("composer.drop")}</span>
    </div>
  )
}
