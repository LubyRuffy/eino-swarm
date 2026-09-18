import { Trash2, X } from "lucide-react"
import { useRef, useState } from "react"

import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { afterImeSettles, enterSendsMessage } from "@/lib/ime"
import type { Followup } from "@/lib/types"
import { useT } from "@/lib/use-t"

/** Codex-style tray: messages waiting for the current turn to finish.
 *  Click a row to edit it; submitting that edit sends it to the back of
 *  the FIFO. Steer injects one into this turn; trash asks, then drops it. */
export function QueueTray({
  items,
  onSteer,
  onDelete,
  onClear,
  onRequeue,
}: {
  items: Followup[]
  onSteer: (id: string) => void
  onDelete: (id: string) => void
  onClear: () => void
  onRequeue: (id: string, text: string) => void
}) {
  const t = useT()
  const [doomed, setDoomed] = useState<Followup>()
  const [clearing, setClearing] = useState(false)
  const [editingId, setEditingId] = useState<string>()

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
            {editingId === item.id ? (
              <QueueEdit
                text={item.text}
                onSubmit={(text) => {
                  onRequeue(item.id, text)
                  setEditingId(undefined)
                }}
                onCancel={() => setEditingId(undefined)}
              />
            ) : (
              <button
                type="button"
                className="min-w-0 flex-1 truncate text-left text-sm hover:underline"
                title={item.text}
                aria-label={t("queue.editNamed", { text: item.text })}
                onClick={() => setEditingId(item.id)}
              >
                {item.text}
              </button>
            )}
            {editingId === item.id ? null : (
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
            )}
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

function QueueEdit({
  text,
  onSubmit,
  onCancel,
}: {
  text: string
  onSubmit: (text: string) => void
  onCancel: () => void
}) {
  const t = useT()
  const [draft, setDraft] = useState(text)
  const composingRef = useRef(false)
  return (
    <Textarea
      data-testid="followup-edit"
      aria-label={t("queue.edit")}
      value={draft}
      autoFocus
      rows={3}
      className="min-w-0 flex-1 px-2 py-1"
      onChange={(e) => setDraft(e.target.value)}
      onCompositionStart={() => {
        composingRef.current = true
      }}
      onCompositionEnd={() => {
        afterImeSettles(() => {
          composingRef.current = false
        })
      }}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault()
          e.stopPropagation()
          onCancel()
          return
        }
        if (!enterSendsMessage(e.nativeEvent, composingRef.current)) return
        e.preventDefault()
        const next = draft.trim()
        if (!next) return
        onSubmit(next)
      }}
    />
  )
}
