import type { ReactNode } from "react"

import { chromeTypeClass } from "@/lib/chrome-type"
import { cn } from "@/lib/utils"

/** One row in the conversation list. Height tracks UI chrome size, not
 *  transcript rem, so content size cannot pack titles into a spreadsheet. */
export const sidebarRowClass = cn(
  chromeTypeClass,
  "sidebar-row group relative flex select-none items-center gap-2 rounded-lg transition-colors",
)

/** Pinned / Projects / Recents / Scheduled labels. Same inline gutter as
 *  the rows underneath, so a section name does not sit in a second grid. */
export const sidebarSectionLabelClass = cn(
  "sidebar-section-label flex min-w-0 flex-1 items-center gap-1 text-left font-medium uppercase tracking-wide text-sidebar-foreground/60",
)

export const sidebarSectionClass = "mb-[var(--sidebar-section-gap)]"

/** Thread rows inside Pinned, Recents, or one folder. The 2px gap is
 *  why the selected pill floats instead of welding to its neighbours. */
export const sidebarStackClass =
  "flex flex-col gap-[var(--sidebar-row-gap)]"

/** Space between project folders. Tighter than a section, looser than a
 *  topic — otherwise ten folders become one grey brick. */
export const sidebarFolderStackClass =
  "flex flex-col gap-[var(--sidebar-folder-gap)]"

/** Folder, running progress, or an empty spacer so titles share a
 *  column with the project directory. Token size, not size-4: that rem
 *  slot shrank with the transcript. */
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
      className="sidebar-kind relative flex shrink-0 items-center justify-center"
    >
      {children}
    </span>
  )
}

/** Live mark on a collapsed folder glyph. The row is chrome type; an
 *  absolutely-positioned span inherits that line box, so a 6px breathe
 *  dot becomes a smear across the icon. Flex + leading-none sizes the
 *  box to the mark; overflow clips the scale keyframe. A pending ask
 *  uses a ping ring that must paint outside that box, so clip is off. */
export function SidebarGlyphMark({
  children,
  clip = true,
}: {
  children: ReactNode
  clip?: boolean
}) {
  return (
    <span
      data-testid="folder-live-mark"
      className={cn(
        "absolute right-0 top-0 flex rounded-full leading-none",
        clip ? "overflow-hidden" : "overflow-visible",
      )}
    >
      {children}
    </span>
  )
}
