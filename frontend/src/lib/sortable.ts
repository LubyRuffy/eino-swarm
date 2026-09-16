import {
  useRef,
  useState,
  type DragEvent,
  type MouseEvent,
  type PointerEvent as ReactPointerEvent,
} from "react"

const MIME = "text/plain"

/** Pixels the pointer must move before a press becomes a reorder.
 *  Native HTML5 `draggable` starts a drag on a twitch and eats the click;
 *  Codex-style lists wait this far so a click still selects. */
export const SORT_ACTIVATE_PX = 8

let listSeq = 0

function fromHandle(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("[data-drag-handle]"))
}

function fromNoDrag(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest("[data-no-drag]"))
}

function hitId(listKey: string, x: number, y: number): string | undefined {
  const el = document.elementFromPoint(x, y)
  if (!(el instanceof Element)) return
  return el.closest(`[data-sortable="${listKey}"]`)?.getAttribute("data-id") ?? undefined
}

function swallowNextClick() {
  const swallow = (e: Event) => {
    e.preventDefault()
    e.stopPropagation()
    window.removeEventListener("click", swallow, true)
  }
  window.addEventListener("click", swallow, true)
  window.setTimeout(() => window.removeEventListener("click", swallow, true), 0)
}

/** Reorder rows the way Codex does: a click selects, a drag past
 *  SORT_ACTIVATE_PX moves. HTML5 `draggable` is never armed on the row —
 *  that API cannot wait for a threshold, so Chrome/WKWebView would eat
 *  the first click. Synthetic dragstart (tests, Playwright) still works
 *  from the grip so the old event sequence keeps passing.
 *
 *  The dragging id lives in a ref as well as state: pointermove and
 *  dragover fire before React re-renders, and a stale closure would treat
 *  the first drop target as idle. Nested lists (a project folder of
 *  conversations) stamp `data-sortable` so a drop does not hit a child. */
export function useSortableList(onMove: (fromId: string, toId: string) => void) {
  const [listKey] = useState(() => `s${++listSeq}`)
  const [dragging, setDragging] = useState<string>()
  const [over, setOver] = useState<string>()
  const draggingRef = useRef<string | undefined>(undefined)
  const armedRef = useRef<string | undefined>(undefined)
  const pressRef = useRef<{ id: string; x: number; y: number } | undefined>(
    undefined,
  )
  const listenersRef = useRef<
    | {
        move: (e: PointerEvent) => void
        up: (e: PointerEvent) => void
      }
    | undefined
  >(undefined)

  const detachPointer = () => {
    const listeners = listenersRef.current
    if (!listeners) return
    window.removeEventListener("pointermove", listeners.move)
    window.removeEventListener("pointerup", listeners.up)
    window.removeEventListener("pointercancel", listeners.up)
    listenersRef.current = undefined
  }

  const stopPointer = () => {
    detachPointer()
    pressRef.current = undefined
  }

  const clear = () => {
    stopPointer()
    draggingRef.current = undefined
    armedRef.current = undefined
    setDragging(undefined)
    setOver(undefined)
  }

  return {
    dragging,
    over,
    bind(id: string, disabled = false) {
      return {
        "data-sortable": listKey,
        "data-dragging": dragging === id ? "true" : undefined,
        "data-over":
          over === id && dragging && dragging !== id ? "true" : undefined,
        onPointerDown: (e: ReactPointerEvent<HTMLElement>) => {
          if (disabled || e.button !== 0 || fromNoDrag(e.target)) return
          e.stopPropagation()
          detachPointer()
          pressRef.current = { id, x: e.clientX, y: e.clientY }
          const move = (ev: PointerEvent) => {
            const press = pressRef.current
            if (!press || press.id !== id) return
            if (!draggingRef.current) {
              const dx = ev.clientX - press.x
              const dy = ev.clientY - press.y
              if (Math.hypot(dx, dy) < SORT_ACTIVATE_PX) return
              draggingRef.current = id
              setDragging(id)
            }
            const next = hitId(listKey, ev.clientX, ev.clientY)
            setOver((cur) => (cur === next ? cur : next))
          }
          const up = (ev: PointerEvent) => {
            const from = draggingRef.current
            const to = from ? hitId(listKey, ev.clientX, ev.clientY) : undefined
            const moved = Boolean(from)
            stopPointer()
            draggingRef.current = undefined
            armedRef.current = undefined
            setDragging(undefined)
            setOver(undefined)
            if (moved) swallowNextClick()
            if (from && to && from !== to) onMove(from, to)
          }
          listenersRef.current = { move, up }
          window.addEventListener("pointermove", move)
          window.addEventListener("pointerup", up)
          window.addEventListener("pointercancel", up)
        },
        onMouseDown: (e: MouseEvent<HTMLElement>) => {
          if (disabled) return
          if (fromNoDrag(e.target)) {
            armedRef.current = undefined
            return
          }
          const hasHandle = Boolean(e.currentTarget.querySelector("[data-drag-handle]"))
          armedRef.current = !hasHandle || fromHandle(e.target) ? id : undefined
        },
        onDragStart: (e: DragEvent<HTMLElement>) => {
          if (disabled || fromNoDrag(e.target)) {
            e.preventDefault()
            return
          }
          const hasHandle = Boolean(e.currentTarget.querySelector("[data-drag-handle]"))
          if (hasHandle && armedRef.current !== id && !fromHandle(e.target)) {
            e.preventDefault()
            return
          }
          e.dataTransfer.setData(MIME, id)
          e.dataTransfer.effectAllowed = "move"
          draggingRef.current = id
          armedRef.current = id
          setDragging(id)
        },
        onDragOver: (e: DragEvent<HTMLElement>) => {
          if (disabled || !draggingRef.current) return
          e.preventDefault()
          e.dataTransfer.dropEffect = "move"
          setOver((cur) => (cur === id ? cur : id))
        },
        onDrop: (e: DragEvent<HTMLElement>) => {
          e.preventDefault()
          const from = e.dataTransfer.getData(MIME) || draggingRef.current
          clear()
          if (from && from !== id) onMove(from, id)
        },
        onDragEnd: () => {
          clear()
        },
      }
    },
  }
}
