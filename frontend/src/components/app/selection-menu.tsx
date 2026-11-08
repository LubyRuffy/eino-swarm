import { MessageSquare, MessageSquarePlus } from "lucide-react"
import { useEffect, useLayoutEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  QUOTE_SELECTION_EVENT,
  menuPosition,
  nextMenuSelection,
  readTextSelection,
  type PageSelection,
} from "@/lib/selection"
import { useT } from "@/lib/use-t"
import { TERMINAL_SELECTION_EVENT, type TerminalSelection } from "@/lib/terminal"

/** Offers quote actions after a selection inside a quote source settles. */
export function SelectionMenu({
  onAdd,
  onAsk,
}: {
  onAdd: (text: string) => void
  onAsk?: (text: string) => void
}) {
  const t = useT()
  const [active, setActive] = useState<PageSelection | null>(null)
  const [clearSelection, setClearSelection] = useState<(() => void) | null>(null)
  const menu = useRef<HTMLDivElement>(null)
  const canAsk = Boolean(onAsk && !clearSelection)

  useEffect(() => {
    let pressed = false
    let terminal: TerminalSelection | null = null

    const fromEventTarget = (target: EventTarget | null) =>
      target instanceof Element && target.closest("[data-testid=selection-menu]")

    const apply = (clearIfEmpty: boolean, announce: boolean) => {
      const live = terminal?.selection ?? readTextSelection()
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
      if (terminal && !(event.target instanceof Node && terminal.source.contains(event.target))) {
        terminal = null
        setClearSelection(null)
      }
      apply(true, true)
    }
    const onSelectionChange = () => {
      // While the pointer is down the range is still being painted; wait.
      if (pressed) return
      apply(false, true)
    }
    const onKeyUp = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        hide()
        return
      }
      if (!fromEventTarget(event.target)) apply(true, true)
    }
    const hide = () => {
      terminal = null
      setClearSelection(null)
      setActive(null)
    }
    const onTerminalSelection = (event: Event) => {
      const next = (event as CustomEvent<TerminalSelection>).detail
      if (!next.selection) {
        if (terminal?.source === next.source) hide()
        return
      }
      window.getSelection()?.removeAllRanges()
      terminal = next
      setClearSelection(() => () => {
        hide()
        next.clear()
      })
      if (!pressed) apply(true, true)
    }
    const onUserPan = (event: Event) => {
      if (fromEventTarget(event.target)) return
      hide()
    }
    // Streaming output may scroll a different pane. Keep the settled snapshot;
    // wheel/touch gestures below already dismiss deliberate user movement.
    const onScroll = () => apply(false, false)

    document.addEventListener("mousedown", onMouseDown)
    document.addEventListener("mouseup", onMouseUp)
    document.addEventListener("selectionchange", onSelectionChange)
    document.addEventListener("keyup", onKeyUp)
    document.addEventListener("scroll", onScroll, true)
    document.addEventListener("wheel", onUserPan, { capture: true, passive: true })
    document.addEventListener("touchmove", onUserPan, { capture: true, passive: true })
    window.addEventListener("resize", hide)
    document.addEventListener(TERMINAL_SELECTION_EVENT, onTerminalSelection)
    return () => {
      document.removeEventListener("mousedown", onMouseDown)
      document.removeEventListener("mouseup", onMouseUp)
      document.removeEventListener("selectionchange", onSelectionChange)
      document.removeEventListener("keyup", onKeyUp)
      document.removeEventListener("scroll", onScroll, true)
      document.removeEventListener("wheel", onUserPan, true)
      document.removeEventListener("touchmove", onUserPan, true)
      window.removeEventListener("resize", hide)
      document.removeEventListener(TERMINAL_SELECTION_EVENT, onTerminalSelection)
    }
  }, [])

  useLayoutEffect(() => {
    const el = menu.current
    if (!active || !el) return
    const { left } = menuPosition(active.rect, windowViewport(), el.offsetWidth)
    el.style.left = `${left}px`
  }, [active, canAsk, t.locale])

  if (!active) return null
  const pos = menuPosition(active.rect, windowViewport())
  const choose = (action: (text: string) => void) => {
    clearSelection?.()
    window.getSelection()?.removeAllRanges()
    setActive(null)
    action(active.text)
  }

  return (
    <div
      ref={menu}
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
        onClick={() => choose(onAdd)}
      >
        <MessageSquarePlus />
        {t("selection.add")}
      </Button>
      {onAsk && canAsk ? (
        <Button
          role="menuitem"
          variant="ghost"
          size="sm"
          onMouseDown={(event) => event.preventDefault()}
          onClick={() => choose(onAsk)}
        >
          <MessageSquare />
          {t("selection.sideChat")}
        </Button>
      ) : null}
    </div>
  )
}

function windowViewport() {
  return { width: window.innerWidth, height: window.innerHeight }
}
