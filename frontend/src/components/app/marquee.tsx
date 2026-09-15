import { useLayoutEffect, useRef, useState } from "react"

import { marqueeDuration, measureOverflow } from "@/lib/marquee"
import { cn } from "@/lib/utils"

/** A live status line. While work is happening the letters get a sweep so
 *  the row does not look frozen; if the line does not fit, it scrolls instead
 *  of hiding behind an ellipsis. Idle lines stay cut. */
export function MarqueeText({
  text,
  active,
  className,
}: {
  text: string
  active?: boolean
  className?: string
}) {
  const outerRef = useRef<HTMLSpanElement>(null)
  const copyRef = useRef<HTMLSpanElement>(null)
  const [overflow, setOverflow] = useState(false)

  useLayoutEffect(() => {
    const outer = outerRef.current
    const copy = copyRef.current
    if (!outer || !copy) return
    const check = () => setOverflow(measureOverflow(outer, copy))
    check()
    if (typeof ResizeObserver === "undefined") return
    const ro = new ResizeObserver(check)
    ro.observe(outer)
    return () => ro.disconnect()
  }, [text])

  const reduce =
    typeof window !== "undefined" &&
    Boolean(window.matchMedia?.("(prefers-reduced-motion: reduce)").matches)
  const scroll = Boolean(active && overflow && text && !reduce)
  const shimmer = Boolean(active && text && !scroll && !reduce)
  const mode = scroll ? "on" : shimmer ? "shimmer" : "off"

  return (
    <span
      ref={outerRef}
      data-testid="marquee"
      data-marquee={mode}
      className={cn("relative min-w-0 flex-1 overflow-hidden", className)}
    >
      {scroll ? (
        <span
          className="inline-flex whitespace-nowrap animate-marquee hover:[animation-play-state:paused]"
          style={{ animationDuration: marqueeDuration(text) }}
        >
          <span ref={copyRef}>{text}</span>
          <span aria-hidden className="pl-8">
            {text}
          </span>
        </span>
      ) : (
        <span
          ref={copyRef}
          className={cn(
            "block truncate",
            shimmer &&
              "animate-shimmer bg-gradient-to-r from-muted-foreground via-foreground to-muted-foreground bg-[length:200%_100%] bg-clip-text text-transparent",
          )}
        >
          {text}
        </span>
      )}
    </span>
  )
}
