import { ChevronDown, ChevronUp, Search, X } from "lucide-react"
import { useCallback, useEffect, useRef, useState, type RefObject } from "react"

import { Button } from "@/components/ui/button"
import { clampMatch, seedQuery, stepMatch } from "@/lib/find"
import { findCountLabelFor } from "@/lib/i18n"
import { IME_KEYCODE } from "@/lib/ime"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

export interface FindController {
  open: boolean
  query: string
  index: number
  total: number
  inputRef: RefObject<HTMLInputElement | null>
  setQuery: (query: string) => void
  setTotal: (total: number) => void
  openFind: () => void
  close: () => void
  next: (delta?: number) => void
  selectQuery: () => void
}

export function useFindController(): FindController {
  const [open, setOpen] = useState(false)
  const [query, setQueryState] = useState("")
  const [index, setIndex] = useState(0)
  const [total, setTotalState] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const queryRef = useRef(query)
  queryRef.current = query

  const setQuery = useCallback((next: string) => {
    setQueryState(next)
    setIndex(0)
  }, [])

  const setTotal = useCallback((next: number) => {
    setTotalState(next)
    setIndex((i) => clampMatch(i, next))
  }, [])

  const selectQuery = useCallback(() => {
    const el = inputRef.current
    if (!el) return
    el.focus()
    el.select()
  }, [])

  const openFind = useCallback(() => {
    const seeded = seedQuery(window.getSelection()?.toString() ?? "", queryRef.current)
    if (seeded !== queryRef.current) {
      setQueryState(seeded)
      setIndex(0)
    }
    setOpen(true)
  }, [])

  const close = useCallback(() => {
    setOpen(false)
  }, [])

  const next = useCallback((delta = 1) => {
    setIndex((i) => stepMatch(i, total, delta))
  }, [total])

  useEffect(() => {
    if (!open) return
    const id = window.requestAnimationFrame(() => selectQuery())
    return () => window.cancelAnimationFrame(id)
  }, [open, selectQuery])

  return {
    open,
    query,
    index,
    total,
    inputRef,
    setQuery,
    setTotal,
    openFind,
    close,
    next,
    selectQuery,
  }
}

export function FindBar({
  query,
  index,
  total,
  inputRef,
  onQuery,
  onNext,
  onClose,
}: {
  query: string
  index: number
  total: number
  inputRef: RefObject<HTMLInputElement | null>
  onQuery: (query: string) => void
  onNext: (delta: number) => void
  onClose: () => void
}) {
  const t = useT()
  const count = findCountLabelFor(index, total, query, t.locale)
  const hasQuery = Boolean(query.trim())
  return (
    <div
      data-find-ignore=""
      data-testid="conversation-find"
      role="search"
      className="absolute right-3 top-3 z-20 w-80 rounded-lg border border-border bg-popover text-popover-foreground shadow-md"
    >
      <div className="flex items-center gap-2 px-2 py-1.5">
        <Search className="size-4 shrink-0 text-muted-foreground" />
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => onQuery(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault()
              onClose()
              return
            }
            if (e.key !== "Enter") return
            if (e.nativeEvent.isComposing || e.keyCode === IME_KEYCODE) return
            e.preventDefault()
            onNext(e.shiftKey ? -1 : 1)
          }}
          aria-label={t("find.label")}
          autoComplete="off"
          autoCorrect="off"
          spellCheck={false}
          className="h-7 min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
        />
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t("find.close")}
          onClick={onClose}
        >
          <X />
        </Button>
      </div>
      {hasQuery ? (
        <div className="flex items-center gap-1 border-t border-border px-2 py-1">
          <p
            data-testid="conversation-find-count"
            aria-live="polite"
            className={cn(
              "min-w-0 flex-1 truncate px-1 text-xs tabular-nums text-muted-foreground",
              total === 0 && "text-destructive",
            )}
          >
            {count}
          </p>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={t("find.prev")}
            disabled={total === 0}
            onClick={() => onNext(-1)}
          >
            <ChevronUp />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            aria-label={t("find.next")}
            disabled={total === 0}
            onClick={() => onNext(1)}
          >
            <ChevronDown />
          </Button>
        </div>
      ) : null}
    </div>
  )
}
