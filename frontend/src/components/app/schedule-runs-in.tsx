import { Check, ChevronDown } from "lucide-react"
import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"

import { settingsSelectTriggerClass } from "@/components/app/settings-field"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import {
  RUNS_IN_NEW,
  pickerThreadLabel,
  pinnedPickerThreads,
  runsInThreadId,
  runsInThreadValue,
  unpinnedPickerGroups,
} from "@/lib/schedule-dest"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

/** Searchable "Runs in" control. Same choices as create: a new
 *  conversation each fire, or one that already exists. */
export function ScheduleRunsInPicker({
  id,
  value,
  disabled,
  expanded,
  onChange,
}: {
  id: string
  value: string
  disabled?: boolean
  expanded?: boolean
  onChange: (value: string) => void
}) {
  const t = useT()
  const threads = useApp((s) => s.threads)
  const projects = useProjects((s) => s.projects)
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState({ right: 0, top: 0 })
  const wrapRef = useRef<HTMLDivElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const pinned = pinnedPickerThreads(threads)
  const groups = unpinnedPickerGroups(threads, projects)
  const untitled = t("sidebar.untitled")
  const selected = threads.find((row) => runsInThreadValue(row.id) === value)
  const label = runsInThreadId(value)
    ? pickerThreadLabel(selected ?? { title: "" }, untitled)
    : t("schedule.runsInNew")

  useLayoutEffect(() => {
    if (!open || !wrapRef.current) return
    const r = wrapRef.current.getBoundingClientRect()
    setPos({ right: window.innerWidth - r.right, top: r.bottom + 4 })
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDown = (event: PointerEvent) => {
      const node = event.target as Node
      if (wrapRef.current?.contains(node) || menuRef.current?.contains(node)) return
      setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      event.preventDefault()
      event.stopPropagation()
      setOpen(false)
    }
    document.addEventListener("pointerdown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      document.removeEventListener("pointerdown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])

  const pick = (next: string) => {
    onChange(next)
    setOpen(false)
  }

  const menu =
    open && typeof document !== "undefined"
      ? createPortal(
          <div
            ref={menuRef}
            className="fixed z-[60] w-72 overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md"
            style={{ right: pos.right, top: pos.top }}
          >
            <Command
              label={t("schedule.runsInSearch")}
              className="h-auto [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground"
            >
              <CommandInput
                placeholder={t("schedule.runsInSearch")}
                aria-label={t("schedule.runsInSearch")}
                className="h-8 py-2 text-xs"
                onKeyDown={(event) => {
                  if (event.key === "Enter") event.preventDefault()
                }}
              />
              <CommandList className="max-h-64">
                <CommandEmpty>{t("schedule.runsInEmpty")}</CommandEmpty>
                <CommandGroup>
                  <CommandItem value={t("schedule.runsInNew")} onSelect={() => pick(RUNS_IN_NEW)}>
                    <Check className={cn("size-3.5", value === RUNS_IN_NEW ? "opacity-100" : "opacity-0")} />
                    <span className="truncate">{t("schedule.runsInNew")}</span>
                  </CommandItem>
                </CommandGroup>
                {pinned.length > 0 ? (
                  <CommandGroup heading={t("schedule.runsInPinned")}>
                    {pinned.map((row) => (
                      <ThreadChoice
                        key={row.id}
                        id={row.id}
                        title={pickerThreadLabel(row, untitled)}
                        selected={value === runsInThreadValue(row.id)}
                        onSelect={() => pick(runsInThreadValue(row.id))}
                      />
                    ))}
                  </CommandGroup>
                ) : null}
                {groups.map((g) => (
                  <CommandGroup key={g.key || "none"} heading={g.label || undefined}>
                    {g.threads.map((row) => (
                      <ThreadChoice
                        key={row.id}
                        id={row.id}
                        title={pickerThreadLabel(row, untitled)}
                        selected={value === runsInThreadValue(row.id)}
                        onSelect={() => pick(runsInThreadValue(row.id))}
                      />
                    ))}
                  </CommandGroup>
                ))}
              </CommandList>
            </Command>
          </div>,
          document.body,
        )
      : null

  return (
    <div ref={wrapRef} className="min-w-0">
      <button
        id={id}
        type="button"
        aria-label={t("schedule.runsIn")}
        aria-expanded={open}
        aria-haspopup="listbox"
        disabled={disabled}
        onClick={() => setOpen((on) => !on)}
        className={cn(
          settingsSelectTriggerClass,
          "inline-flex items-center justify-between gap-1",
          expanded ? "max-w-[22rem]" : "max-w-[13rem]",
        )}
      >
        <span className="truncate">{label}</span>
        <ChevronDown className="size-3.5 shrink-0 opacity-60" />
      </button>
      {menu}
    </div>
  )
}

function ThreadChoice({
  id,
  title,
  selected,
  onSelect,
}: {
  id: string
  title: string
  selected: boolean
  onSelect: () => void
}) {
  return (
    <CommandItem value={`${title} ${id}`} onSelect={onSelect}>
      <Check className={cn("size-3.5", selected ? "opacity-100" : "opacity-0")} />
      <span className="truncate">{title}</span>
    </CommandItem>
  )
}
