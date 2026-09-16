import { ChevronRight } from "lucide-react"
import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

/** Pinned, Projects, Recents: a labelled disclosure. The chevron after
 *  the name is hover-only while the section is open, so an expanded
 *  list is not a column of arrows. */
export function SidebarSection({
  testId,
  label,
  open,
  onToggle,
  actions,
  children,
}: {
  testId: string
  label: string
  open: boolean
  onToggle: () => void
  actions?: ReactNode
  children?: ReactNode
}) {
  return (
    <section className="mb-2" data-testid={testId}>
      <div className="group flex h-7 items-center pr-2">
        <button
          type="button"
          aria-expanded={open}
          onClick={onToggle}
          className="flex h-7 min-w-0 flex-1 items-center gap-0.5 px-2 text-left text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/60"
        >
          <span className="truncate">{label}</span>
          <ChevronRight
            data-testid="section-fold"
            className={cn(
              "size-3 shrink-0 transition-opacity transition-transform",
              open && "rotate-90",
              open
                ? "opacity-0 group-hover:opacity-100 group-focus-within:opacity-100"
                : "opacity-100",
            )}
            aria-hidden="true"
          />
        </button>
        {actions}
      </div>
      {open ? children : null}
    </section>
  )
}
