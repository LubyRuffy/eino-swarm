import { useLayoutEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react"

import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import {
  TURN_NAV_MIN,
  activeNavId,
  offsetInScroller,
  previewText,
  turnNavSelector,
  type TurnNavItem,
} from "@/lib/turn-nav"

/** Compact tick cluster in the middle of the transcript, not a full-height
 *  scrollbar. Hover opens the list of the user's own messages. */
export function TurnNav({
  items,
  scrollerRef,
  onJump,
}: {
  items: TurnNavItem[]
  scrollerRef: RefObject<HTMLElement | null>
  onJump: (id: string) => void
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [hovered, setHovered] = useState<string>()
  const [active, setActive] = useState<string>()

  const itemKey = items.map((i) => `${i.id}\0${i.text}`).join("\n")
  const itemsRef = useRef(items)
  itemsRef.current = items

  useLayoutEffect(() => {
    const root = scrollerRef.current
    if (!root) return
    const measure = () => {
      const list = itemsRef.current
      const offsets = list.map((item) => {
        const el = root.querySelector(turnNavSelector(item.id))
        return el instanceof HTMLElement ? offsetInScroller(el, root) : 0
      })
      setActive(
        activeNavId(
          list.map((item, i) => ({ id: item.id, top: offsets[i] ?? 0 })),
          root.scrollTop,
          root.clientHeight,
          root.scrollHeight,
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
  }, [scrollerRef, itemKey])

  if (items.length < TURN_NAV_MIN) return null

  const highlight = hovered ?? active ?? items[0]?.id

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
        <div className="relative flex max-h-48 flex-col items-center gap-2 overflow-y-auto">
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
                className="relative flex h-3 w-4 shrink-0 items-center justify-center"
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
          data-testid="turn-nav-list"
          className="absolute left-8 top-1/2 z-20 w-64 max-h-96 -translate-y-1/2 overflow-y-auto rounded-xl border border-border bg-popover p-1.5 shadow-md thin-scrollbar"
        >
          {items.map((item) => {
            const hot = item.id === highlight
            return (
              <button
                key={item.id}
                type="button"
                tabIndex={-1}
                className={cn(
                  "mb-0.5 w-full truncate rounded-lg px-3 py-2 text-left text-[13px] leading-5 last:mb-0",
                  hot
                    ? "bg-secondary text-secondary-foreground"
                    : "text-muted-foreground hover:bg-accent hover:text-accent-foreground",
                )}
                onMouseEnter={() => setHovered(item.id)}
                onClick={() => onJump(item.id)}
              >
                {previewText(item.text)}
              </button>
            )
          })}
        </div>
      ) : null}
    </nav>
  )
}
