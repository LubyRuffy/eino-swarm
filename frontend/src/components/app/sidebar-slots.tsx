import type { ReactNode } from "react"

import { cn } from "@/lib/utils"

/** One row in the conversation list. The grip overlays the left padding so
 *  it does not indent the icon or the title. */
export const sidebarRowClass =
  "group relative flex h-7 select-none items-center gap-1 rounded-md pl-2 pr-2 text-sm transition-colors"

export function SidebarGripSlot({
  handle,
  onClick,
  children,
}: {
  handle?: boolean
  onClick?: () => void
  children?: ReactNode
}) {
  if (!handle && !children) return null
  return (
    <span
      {...(handle ? { "data-drag-handle": "" } : {})}
      className={cn(
        "absolute inset-y-0 left-0 z-10 flex w-3 items-center justify-center",
        handle && "cursor-grab opacity-0 group-hover:opacity-50",
      )}
      aria-hidden="true"
      onClick={onClick}
    >
      {children}
    </span>
  )
}

/** Folder, or an empty spacer so conversation titles share a column
 *  with the project name. */
export function SidebarKindSlot({
  testId = "row-kind",
  children,
}: {
  testId?: string
  children?: ReactNode
}) {
  return (
    <span
      data-testid={testId}
      className="relative flex size-4 shrink-0 items-center justify-center"
    >
      {children}
    </span>
  )
}
