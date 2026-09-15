import * as React from "react"

import { cn } from "@/lib/utils"

/** A disclosure row: a summary line that expands. Radix's collapsible brings
 *  animation machinery this UI does not need — the transcript has hundreds of
 *  these and they must be cheap. */
export function Disclosure({
  open,
  onOpenChange,
  summary,
  children,
  className,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  summary: React.ReactNode
  children?: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn("group", className)}>
      <button
        type="button"
        onClick={() => onOpenChange(!open)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {summary}
      </button>
      {open && children ? <div className="mt-1 pl-2">{children}</div> : null}
    </div>
  )
}
