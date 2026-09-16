import { Flag, Minimize2 } from "lucide-react"

import { cn } from "@/lib/utils"
import type { SlashCommand } from "@/lib/slash"
import { useT } from "@/lib/use-t"

const ICONS = {
  goal: Flag,
  compact: Minimize2,
} as const

/** Codex/Cursor-style list that sits on the composer while the draft is `/…`.
 *  The textarea owns the keyboard; this only paints the highlighted row. */
export function SlashMenu({
  items,
  activeIndex,
  onHover,
  onSelect,
}: {
  items: SlashCommand[]
  activeIndex: number
  onHover: (index: number) => void
  onSelect: (cmd: SlashCommand) => void
}) {
  const t = useT()
  if (items.length === 0) return null
  return (
    <div
      id="slash-menu"
      data-testid="slash-menu"
      role="listbox"
      aria-label={t("slash.commands")}
      className="absolute inset-x-0 bottom-full z-20 mb-2 overflow-hidden rounded-2xl border border-border bg-popover text-popover-foreground shadow-lg"
    >
      <ul className="max-h-64 overflow-y-auto p-1 thin-scrollbar">
        {items.map((cmd, i) => {
          const Icon = ICONS[cmd.id]
          const active = i === activeIndex
          return (
            <li key={cmd.id}>
              <button
                type="button"
                role="option"
                aria-selected={active}
                data-testid={`slash-command-${cmd.id}`}
                className={cn(
                  "flex w-full items-center gap-2 rounded-xl px-2 py-2 text-left text-sm",
                  active ? "bg-accent text-accent-foreground" : "hover:bg-accent/60",
                )}
                onMouseEnter={() => onHover(i)}
                onClick={() => onSelect(cmd)}
              >
                <Icon className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                <span className="font-medium">{cmd.name}</span>
                <span className="min-w-0 flex-1 truncate text-muted-foreground">
                  {cmd.description}
                </span>
                {cmd.hint ? (
                  <span className="shrink-0 text-[11px] text-muted-foreground">
                    {cmd.hint}
                  </span>
                ) : null}
              </button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}
