import { useLayoutEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react"

import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import {
  TURN_NAV_LIST_PREVIEW,
  TURN_NAV_MIN,
  activeNavId,
  offsetInScroller,
  packTurnNavTicks,
  previewText,
  turnNavSelector,
  type TurnNavItem,
} from "@/lib/turn-nav"

function attrEscape(value: string): string {
  return typeof CSS !== "undefined" && typeof CSS.escape === "function"
    ? CSS.escape(value)
    : value.replace(/\\/g, "\\\\").replace(/"/g, '\\"')
}

/** Compact tick cluster in the middle of the transcript, not a full-height
 *  scrollbar. Hover opens the list of the human's own sends — labels, not
 *  a card of fake bubbles. */
export function TurnNav({
  items,
  scrollerRef,
  onJump,
  pinned = false,
}: {
  items: TurnNavItem[]
  scrollerRef: RefObject<HTMLElement | null>
  onJump: (id: string) => void
  /** Following the live edge: the latest turn, even before layout has
   *  scrolled the opener to the bottom. */
  pinned?: boolean
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [hovered, setHovered] = useState<string>()
  const [active, setActive] = useState<string | undefined>(() => items.at(-1)?.id)

  const itemKey = items.map((i) => `${i.id}\0${i.text}`).join("\n")
  const itemsRef = useRef(items)
  itemsRef.current = items
  const listRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const root = scrollerRef.current
    if (!root) return
    const measure = () => {
      const list = itemsRef.current
      if (pinned) {
        setActive(list.at(-1)?.id)
        return
      }
      setActive(
        activeNavId(
          list.map((item) => {
            const el = root.querySelector(turnNavSelector(item.id))
            return el instanceof HTMLElement
              ? { id: item.id, top: offsetInScroller(el, root) }
              : { id: item.id }
          }),
          root.scrollTop,
          root.clientHeight,
          root.scrollHeight,
          pinned,
        ),
      )
    }
    measure()
    if (typeof ResizeObserver === "undefined") {
      root.addEventListener("scroll", measure, { passive: true })
      return () => root.removeEventListener("scroll", measure)
    }
    const ro = new ResizeObserver(measure)
    ro.observe(root)
    const inner = root.firstElementChild
    if (inner) ro.observe(inner)
    root.addEventListener("scroll", measure, { passive: true })
    return () => {
      ro.disconnect()
      root.removeEventListener("scroll", measure)
    }
    // itemKey is id+text; a streamed answer must not rebuild the rail.
  }, [scrollerRef, itemKey, pinned])

  const highlight = hovered ?? active ?? items.at(-1)?.id

  useLayoutEffect(() => {
    if (!open || !highlight) return
    const row = listRef.current?.querySelector(
      `[data-turn-nav-row="${attrEscape(highlight)}"]`,
    )
    if (row instanceof HTMLElement && typeof row.scrollIntoView === "function") {
      row.scrollIntoView({ block: "nearest" })
    }
  }, [open, highlight])

  if (items.length < TURN_NAV_MIN) return null

  const packTicks = packTurnNavTicks(items.length)

  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== "ArrowDown" && event.key !== "ArrowUp" && event.key !== "Home" && event.key !== "End") {
      return
    }
    event.preventDefault()
    const from = highlight ? items.findIndex((i) => i.id === highlight) : 0
    let next = from
    if (event.key === "ArrowDown") next = Math.min(items.length - 1, from + 1)
    if (event.key === "ArrowUp") next = Math.max(0, from - 1)
    if (event.key === "Home") next = 0
    if (event.key === "End") next = items.length - 1
    const id = items[next]?.id
    if (!id) return
    setHovered(id)
    onJump(id)
  }

  return (
    <nav
      data-testid="turn-nav"
      aria-label={t("nav.jump")}
      className="absolute left-0 top-1/2 z-20 flex -translate-y-1/2"
      onMouseEnter={() => setOpen(true)}
      onMouseLeave={() => {
        setOpen(false)
        setHovered(undefined)
      }}
      onFocus={() => setOpen(true)}
      onBlur={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setOpen(false)
          setHovered(undefined)
        }
      }}
      onKeyDown={onKeyDown}
    >
      <div className="relative flex w-8 flex-col items-center py-1.5">
        <div className="absolute inset-y-1.5 left-1/2 w-px -translate-x-1/2 bg-muted-foreground/30" />
        <div
          data-testid="turn-nav-ticks"
          className={cn(
            "relative flex w-full flex-col items-center",
            packTicks ? "h-56" : "gap-2",
          )}
        >
          {items.map((item) => {
            const current = item.id === active
            const hot = item.id === highlight
            return (
              <button
                key={item.id}
                type="button"
                aria-label={previewText(item.text)}
                aria-current={current ? "true" : undefined}
                data-turn-nav-tick={item.id}
                className={cn(
                  "relative flex w-4 items-center justify-center",
                  packTicks ? "min-h-0 flex-1" : "h-3 shrink-0",
                )}
                onMouseEnter={() => setHovered(item.id)}
                onFocus={() => setHovered(item.id)}
                onClick={() => onJump(item.id)}
              >
                <span
                  className={cn(
                    "block h-0.5 rounded-full transition-colors",
                    hot || current ? "w-3.5 bg-foreground" : "w-2.5 bg-muted-foreground/70",
                  )}
                />
              </button>
            )
          })}
        </div>
      </div>
      {open ? (
        <div
          ref={listRef}
          data-testid="turn-nav-list"
          className="absolute left-8 top-1/2 z-20 w-96 max-h-[min(28rem,70vh)] max-w-[calc(100vw-3.5rem)] -translate-y-1/2 overflow-y-auto bg-background py-1 thin-scrollbar"
        >
          {items.map((item) => {
            const hot = item.id === highlight
            return (
              <button
                key={item.id}
                type="button"
                tabIndex={-1}
                data-turn-nav-row={item.id}
                className={cn(
                  "block w-full px-3 py-1.5 text-left text-[13px] leading-5",
                  hot
                    ? "text-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
                onMouseEnter={() => setHovered(item.id)}
                onClick={() => onJump(item.id)}
              >
                <span className="line-clamp-2 break-words">
                  {previewText(item.text, TURN_NAV_LIST_PREVIEW)}
                </span>
              </button>
            )
          })}
        </div>
      ) : null}
    </nav>
  )
}
