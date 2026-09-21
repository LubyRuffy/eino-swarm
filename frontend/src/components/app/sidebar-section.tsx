import { ChevronRight } from "lucide-react"
import type { ReactNode } from "react"

import {
  sidebarSectionClass,
  sidebarSectionLabelClass,
  sidebarStackClass,
} from "@/components/app/sidebar-slots"
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
    <section className={sidebarSectionClass} data-testid={testId}>
      <div className="group flex h-[var(--sidebar-section-label-height)] items-center pr-1">
        <button
          type="button"
          aria-expanded={open}
          onClick={onToggle}
          className={sidebarSectionLabelClass}
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
      {open ? <div className={sidebarStackClass}>{children}</div> : null}
    </section>
  )
}
