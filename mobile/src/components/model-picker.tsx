import { useEffect, useRef } from "react"
import { createPortal } from "react-dom"
import { Check, X } from "lucide-react"

import { t } from "@/lib/i18n"
import type { ModelChoice } from "@/lib/rpc"

/** Android renders native select options outside the WebView's CSS. Keep the
 * model catalog in the app so its type scale and wrapping match the chat. */
export function ModelPicker({
  models,
  providerId,
  model,
  onSelect,
  onClose,
}: {
  models: ModelChoice[]
  providerId: string
  model: string
  onSelect: (providerId: string, model: string) => void
  onClose: () => void
}) {
  const closeRef = useRef(onClose)
  const closeButton = useRef<HTMLButtonElement>(null)
  closeRef.current = onClose

  useEffect(() => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    closeButton.current?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation()
        closeRef.current()
      }
    }
    window.addEventListener("keydown", onKey)
    const previousBack = window.__zwaiAndroidBack
    const back = () => {
      closeRef.current()
      return true
    }
    window.__zwaiAndroidBack = back
    return () => {
      window.removeEventListener("keydown", onKey)
      if (window.__zwaiAndroidBack === back) window.__zwaiAndroidBack = previousBack
      previousFocus?.focus()
    }
  }, [])

  const groups: { label: string; rows: ModelChoice[] }[] = []
  for (const row of models) {
    if (!row.model) continue
    const label = row.provider_label || row.provider_id
    const group = groups.find((item) => item.label === label)
    if (group) group.rows.push(row)
    else groups.push({ label, rows: [row] })
  }

  return createPortal(
    <div className="fixed inset-0 z-50 flex items-end justify-center">
      <button
        type="button"
        className="absolute inset-0 bg-foreground/40"
        aria-label={t("home.close")}
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={t("composer.model")}
        className="relative z-10 flex max-h-[80vh] w-full flex-col rounded-t-2xl border border-border bg-background pb-[env(safe-area-inset-bottom)]"
      >
        <div className="flex items-center justify-between border-b border-border px-4 py-2">
          <h2 className="text-base font-semibold">{t("composer.model")}</h2>
          <button
            ref={closeButton}
            type="button"
            aria-label={t("home.close")}
            className="flex size-10 items-center justify-center rounded-full text-muted-foreground active:bg-accent"
            onClick={onClose}
          >
            <X className="size-5" aria-hidden />
          </button>
        </div>
        <div role="radiogroup" aria-label={t("composer.model")} className="min-h-0 overflow-y-auto overscroll-contain px-2 py-2">
          {groups.map((group) => (
            <div key={group.label}>
              <h3 className="px-3 py-2 text-xs font-semibold text-muted-foreground">{group.label}</h3>
              {group.rows.map((row) => {
                const selected = row.provider_id === providerId && row.model === model
                return (
                  <button
                    key={row.provider_id + "\t" + row.model}
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    className="flex min-h-11 w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm text-foreground active:bg-accent"
                    onClick={() => onSelect(row.provider_id, row.model)}
                  >
                    <span className="min-w-0 flex-1 break-all">{row.model}</span>
                    {selected ? <Check className="size-4 shrink-0 text-primary" aria-hidden /> : null}
                  </button>
                )
              })}
            </div>
          ))}
        </div>
      </div>
    </div>,
    document.body,
  )
}
