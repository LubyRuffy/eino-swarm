import { MessageSquarePlus } from "lucide-react"
import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  QUOTE_SELECTION_EVENT,
  menuPosition,
  nextMenuSelection,
  readTextSelection,
  type PageSelection,
} from "@/lib/selection"
import { useT } from "@/lib/use-t"

/** Floating "Add to chat" pill. Shown after a selection inside a quote
 *  source settles (pointer up, or a Shift+arrow). Clicking stores the
 *  snippet on the composer instead of dumping it into the textarea. */
export function SelectionMenu({ onAdd }: { onAdd: (text: string) => void }) {
  const t = useT()
  const [active, setActive] = useState<PageSelection | null>(null)

  useEffect(() => {
    let pressed = false

    const fromEventTarget = (target: EventTarget | null) =>
      target instanceof Element && target.closest("[data-testid=selection-menu]")

    const apply = (clearIfEmpty: boolean, announce: boolean) => {
      const live = readTextSelection()
      if (announce && live) {
        document.dispatchEvent(new Event(QUOTE_SELECTION_EVENT))
      }
      setActive((current) => nextMenuSelection(current, live, clearIfEmpty))
    }

    const onMouseDown = (event: MouseEvent) => {
      if (fromEventTarget(event.target)) return
      pressed = true
    }
    const onMouseUp = (event: MouseEvent) => {
      if (fromEventTarget(event.target)) return
      pressed = false
      apply(true, true)
    }
    const onSelectionChange = () => {
      // While the pointer is down the range is still being painted; wait.
      if (pressed) return
      apply(false, true)
    }
    const onKeyUp = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setActive(null)
        return
      }
      apply(true, true)
    }
    const hide = () => setActive(null)
    const onUserPan = (event: Event) => {
      if (fromEventTarget(event.target)) return
      hide()
    }
    const onScroll = () => apply(false, false)

    document.addEventListener("mousedown", onMouseDown)
    document.addEventListener("mouseup", onMouseUp)
    document.addEventListener("selectionchange", onSelectionChange)
    document.addEventListener("keyup", onKeyUp)
    document.addEventListener("scroll", onScroll, true)
    document.addEventListener("wheel", onUserPan, { capture: true, passive: true })
    document.addEventListener("touchmove", onUserPan, { capture: true, passive: true })
    window.addEventListener("resize", hide)
    return () => {
      document.removeEventListener("mousedown", onMouseDown)
      document.removeEventListener("mouseup", onMouseUp)
      document.removeEventListener("selectionchange", onSelectionChange)
      document.removeEventListener("keyup", onKeyUp)
      document.removeEventListener("scroll", onScroll, true)
      document.removeEventListener("wheel", onUserPan, true)
      document.removeEventListener("touchmove", onUserPan, true)
      window.removeEventListener("resize", hide)
    }
  }, [])

  if (!active) return null
  const pos = menuPosition(active.rect, {
    width: window.innerWidth,
    height: window.innerHeight,
  })

  return (
    <div
      data-testid="selection-menu"
      role="menu"
      aria-label={t("selection.menu")}
      className="fixed z-50 flex rounded-md border border-border bg-popover p-0.5 text-popover-foreground shadow-md"
      style={{
        top: pos.top,
        left: pos.left,
        transform: pos.place === "above" ? "translate(-50%, -100%)" : "translate(-50%, 0)",
      }}
    >
      <Button
        role="menuitem"
        variant="ghost"
        size="sm"
        onMouseDown={(event) => event.preventDefault()}
        onClick={() => {
          onAdd(active.text)
          window.getSelection()?.removeAllRanges()
          setActive(null)
        }}
      >
        <MessageSquarePlus />
        {t("selection.add")}
      </Button>
    </div>
  )
}
