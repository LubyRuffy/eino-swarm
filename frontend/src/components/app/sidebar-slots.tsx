import type { ReactNode } from "react"

import { chromeTypeClass } from "@/lib/chrome-type"
import { cn } from "@/lib/utils"

/** One row in the conversation list. Same type as Settings chrome. */
export const sidebarRowClass = cn(
  chromeTypeClass,
  "group relative flex h-7 select-none items-center gap-1 rounded-md pl-2 pr-2 transition-colors",
)

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
