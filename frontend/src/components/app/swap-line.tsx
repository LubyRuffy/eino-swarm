import { useEffect, useRef, useState } from "react"

import { MarqueeText } from "@/components/app/marquee"
import { cn } from "@/lib/utils"

/** One-line viewport. A new activity slides the old line up and the new
 *  one in from below. Same id updates in place so streamed tokens do not
 *  bounce the row. */
export function SwapLine({
  itemKey,
  text,
  active,
  className,
}: {
  itemKey: string
  text: string
  active?: boolean
  className?: string
}) {
  const shownKey = useRef(itemKey)
  const [current, setCurrent] = useState({ key: itemKey, text })
  const [outgoing, setOutgoing] = useState<{ key: string; text: string } | null>(
    null,
  )

  useEffect(() => {
    if (shownKey.current === itemKey) {
      setCurrent({ key: itemKey, text })
      return
    }
    setOutgoing({ key: shownKey.current, text: current.text })
    setCurrent({ key: itemKey, text })
    shownKey.current = itemKey
  }, [itemKey, text, current.text])

  useEffect(() => {
    if (!outgoing) return
    const id = window.setTimeout(() => setOutgoing(null), 280)
    return () => window.clearTimeout(id)
  }, [outgoing])

  const reduce =
    typeof window !== "undefined" &&
    Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)

  return (
    <span
      data-testid="swap-line"
      data-swap-key={current.key}
      data-find-ignore=""
      className={cn(
        // w-0 flex-1: the slot is the leftover row, not the line's
        // min-content. Without that the letters never overflow and the
        // left-to-right marquee never starts.
        "relative block h-5 w-0 min-w-0 flex-1 overflow-hidden",
        className,
      )}
    >
      {outgoing && !reduce ? (
        <span
          aria-hidden
          className="absolute inset-0 flex min-w-0 items-center overflow-hidden animate-swap-out"
        >
          <MarqueeText text={outgoing.text} active={false} className="w-full max-w-full" />
        </span>
      ) : null}
      <span
        className={cn(
          "absolute inset-0 flex min-w-0 items-center overflow-hidden",
          outgoing && !reduce && "animate-swap-in",
        )}
      >
        <MarqueeText text={current.text} active={active} className="w-full max-w-full" />
      </span>
    </span>
  )
}
