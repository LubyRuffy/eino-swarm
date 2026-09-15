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
  failed,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  summary: React.ReactNode
  children?: React.ReactNode
  className?: string
  failed?: boolean
}) {
  return (
    <div className={cn("group", className)}>
      <button
        type="button"
        onClick={() => onOpenChange(!open)}
        aria-expanded={open}
        aria-invalid={failed || undefined}
        className={cn(
          "flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
          failed
            ? "text-destructive hover:text-destructive"
            : "text-muted-foreground hover:text-foreground",
        )}
      >
        {summary}
      </button>
      {open && children ? <div className="mt-1 pl-2">{children}</div> : null}
    </div>
  )
}
