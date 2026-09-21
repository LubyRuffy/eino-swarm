import { X } from "lucide-react"

import { ScanScreen } from "@/components/scan-screen"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"

export function AddHostSheet({
  onURI,
  busy,
  error,
  onClose,
}: {
  onURI: (uri: string) => void
  busy?: boolean
  error?: string
  onClose: () => void
}) {
  return (
    <div className="fixed inset-0 z-40 flex flex-col justify-end">
      <button
        type="button"
        className="absolute inset-0 bg-foreground/40"
        aria-label={t("home.close")}
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-labelledby="add-host-title"
        className="relative z-10 max-h-[85%] overflow-y-auto rounded-t-2xl border border-border bg-background pb-[env(safe-area-inset-bottom)]"
      >
        <div className="flex items-center justify-between px-5 pb-1 pt-3">
          <h2 id="add-host-title" className="text-lg font-semibold tracking-tight">
            {t("home.addHost")}
          </h2>
          <Button
            type="button"
            variant="ghost"
            className="size-10 shrink-0 px-0"
            aria-label={t("home.close")}
            onClick={onClose}
          >
            <X className="size-5" aria-hidden />
          </Button>
        </div>
        <ScanScreen embedded onURI={onURI} busy={busy} error={error} />
      </div>
    </div>
  )
}
