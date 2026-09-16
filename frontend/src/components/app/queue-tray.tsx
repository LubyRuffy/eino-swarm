import { Trash2, X } from "lucide-react"
import { useState } from "react"

import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { Button } from "@/components/ui/button"
import type { Followup } from "@/lib/types"
import { useT } from "@/lib/use-t"

/** Cursor-style tray: messages waiting for the current turn to finish.
 *  Steer injects one into that turn; trash asks, then drops it; the header
 *  clears all. */
export function QueueTray({
  items,
  onSteer,
  onDelete,
  onClear,
}: {
  items: Followup[]
  onSteer: (id: string) => void
  onDelete: (id: string) => void
  onClear: () => void
}) {
  const t = useT()
  const [doomed, setDoomed] = useState<Followup>()
  const [clearing, setClearing] = useState(false)

  if (items.length === 0) return null
  return (
    <div
      data-testid="followup-queue"
      className="mb-2 rounded-2xl border border-border bg-card px-3 py-2 shadow-lg"
    >
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium text-muted-foreground">
          {t("queue.count", { n: items.length })}
        </p>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t("queue.clear")}
          onClick={() => setClearing(true)}
        >
          <X />
        </Button>
      </div>
      <ul className="mt-1 space-y-2">
        {items.map((item) => (
          <li key={item.id} className="flex items-start gap-2">
            <p className="min-w-0 flex-1 truncate text-sm" title={item.text}>
              {item.text}
            </p>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 px-2"
              aria-label={t("queue.steerNamed", { text: item.text })}
              onClick={() => onSteer(item.id)}
            >
              {t("queue.steer")}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("queue.removeNamed", { text: item.text })}
              onClick={() => setDoomed(item)}
            >
              <Trash2 />
            </Button>
          </li>
        ))}
      </ul>
      <ConfirmDeleteDialog
        open={Boolean(doomed)}
        title={t("queue.deleteTitle")}
        description={t("queue.deleteDesc")}
        confirmLabel={t("queue.deleteConfirm")}
        cancelLabel={t("confirm.cancel")}
        onOpenChange={(open) => {
          if (!open) setDoomed(undefined)
        }}
        onConfirm={() => {
          if (doomed) onDelete(doomed.id)
        }}
      />
      <ConfirmDeleteDialog
        open={clearing}
        title={t("queue.clearTitle")}
        description={t("queue.clearDesc", { n: items.length })}
        confirmLabel={t("queue.clearConfirm")}
        cancelLabel={t("confirm.cancel")}
        onOpenChange={setClearing}
        onConfirm={onClear}
      />
    </div>
  )
}
