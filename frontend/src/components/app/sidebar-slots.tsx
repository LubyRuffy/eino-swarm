import type { ReactNode } from "react"

/** One row in the conversation list. */
export const sidebarRowClass =
  "group relative flex h-7 select-none items-center gap-1 rounded-md pl-2 pr-2 text-sm transition-colors"

/** Folder, running progress, or an empty spacer so titles share a
 *  column with the project directory. */
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
