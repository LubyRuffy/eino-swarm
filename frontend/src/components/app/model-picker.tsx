import { Check, ChevronDown, Loader2, Pencil, RefreshCw } from "lucide-react"
import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { groupModels, modelChoiceId } from "@/lib/models"
import { chromeTypeClass } from "@/lib/chrome-type"
import type { ModelInfo } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"

export function ModelPicker({
  models,
  providerId,
  model,
  onChange,
  onRefresh,
  onEdit,
}: {
  models: ModelInfo[]
  providerId?: string
  model?: string
  onChange: (providerId: string, model: string) => void
  onRefresh?: () => Promise<void>
  onEdit?: () => void
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [pos, setPos] = useState({ left: 0, bottom: 0 })
  const wrapRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const ready = models.filter((m) => m.ready && m.model)
  const groups = groupModels(ready)
  const selected =
    ready.find((m) => m.provider_id === providerId && m.model === model) ??
    ready.find((m) => m.provider_id === providerId && m.default) ??
    ready.find((m) => m.provider_id === providerId) ??
    ready.find((m) => m.default) ??
    ready[0]
  const label = selected?.label || selected?.model || t("model.fallback")

  useLayoutEffect(() => {
    if (!open || !wrapRef.current) return
    const r = wrapRef.current.getBoundingClientRect()
    setPos({ left: r.left, bottom: window.innerHeight - r.top + 4 })
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDown = (event: PointerEvent) => {
      const t = event.target as Node
      if (wrapRef.current?.contains(t) || menuRef.current?.contains(t)) return
      setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("pointerdown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("pointerdown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])

  const menu =
    open && typeof document !== "undefined"
      ? createPortal(
          <div
            ref={menuRef}
            className="fixed z-[60] w-72 overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md"
            style={{ left: pos.left, bottom: pos.bottom }}
          >
            <Command
              label={t("model.search")}
              className="h-auto [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-muted-foreground"
            >
              <CommandInput
                placeholder={t("model.search")}
                aria-label={t("model.search")}
                className="h-8 py-2 text-xs"
              />
              <CommandList className="max-h-64">
                <CommandEmpty>
                  {ready.length === 0
                    ? t("model.emptyNone")
                    : t("model.emptyFilter")}
                </CommandEmpty>
                {groups.map((g) => (
                  <CommandGroup key={g.providerId} heading={g.label}>
                    {g.models.map((m) => {
                      const id = m.id || modelChoiceId(m.provider_id, m.model)
                      const isSelected =
                        selected?.provider_id === m.provider_id &&
                        selected?.model === m.model
                      return (
                        <CommandItem
                          key={id}
                          value={`${g.providerId} ${g.label} ${m.label || m.model} ${m.model}`}
                          onSelect={() => {
                            onChange(m.provider_id, m.model)
                            setOpen(false)
                          }}
                        >
                          <Check
                            className={cn(
                              "size-3.5",
                              isSelected ? "opacity-100" : "opacity-0",
                            )}
                          />
                          <span className="truncate">{m.label || m.model}</span>
                        </CommandItem>
                      )
                    })}
                  </CommandGroup>
                ))}
              </CommandList>
            </Command>
            {onRefresh || onEdit ? (
              <div className="flex flex-col border-t">
                {onRefresh ? (
                  <button
                    type="button"
                    className="flex h-8 items-center gap-2 px-3 text-xs text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-50"
                    disabled={busy}
                    onClick={() => {
                      setBusy(true)
                      void onRefresh()
                        .catch(() => undefined)
                        .finally(() => setBusy(false))
                    }}
                  >
                    {busy ? (
                      <Loader2 className="size-3.5 animate-spin" />
                    ) : (
                      <RefreshCw className="size-3.5" />
                    )}
                    {t("model.refresh")}
                  </button>
                ) : null}
                {onEdit ? (
                  <button
                    type="button"
                    className="flex h-8 items-center gap-2 px-3 text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
                    onClick={() => {
                      setOpen(false)
                      onEdit()
                    }}
                  >
                    <Pencil className="size-3.5" />
                    {t("model.edit")}
                  </button>
                ) : null}
              </div>
            ) : null}
          </div>,
          document.body,
        )
      : null

  return (
    <div ref={wrapRef} className="relative">
      <button
        type="button"
        aria-label={t("composer.model")}
        aria-expanded={open}
        aria-haspopup="listbox"
        className={cn(
          "inline-flex h-7 max-w-[14rem] items-center gap-1.5 rounded-md px-2 hover:bg-accent",
          chromeTypeClass,
          open && "bg-accent",
        )}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="truncate">{label}</span>
        <ChevronDown className="size-3.5 text-muted-foreground" />
      </button>
      {menu}
    </div>
  )
}
