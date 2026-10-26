import { Brain, ChevronDown, ChevronRight, Loader2 } from "lucide-react"
import { useCallback, useEffect, useRef, useState } from "react"

import { MarqueeText } from "@/components/app/marquee"
import { Disclosure } from "@/components/ui/collapsible"
import {
  thoughtExpanded,
  thoughtFollowsStream,
  thoughtHasHiddenPrefix,
} from "@/lib/thought-scroll"
import type { Block } from "@/lib/transcript"
import { useT } from "@/lib/use-t"

export function Reasoning({ block, reveal, defaultCollapsed = false }: {
  block: Block
  reveal?: boolean
  defaultCollapsed?: boolean
}) {
  const t = useT()
  const [choice, setChoice] = useState<boolean | null>(null)
  // User view keeps process detail folded; inspection views can watch it live.
  // A reader's choice survives tokens, and Find still reveals matching text.
  const expanded = thoughtExpanded(Boolean(block.streaming) && !defaultCollapsed, choice) || Boolean(reveal)
  return (
    <Disclosure
      open={expanded}
      onOpenChange={(next) => setChoice(next)}
      testId="thought-toggle"
      summary={
        <>
          {block.streaming ? (
            <Loader2 className="size-3.5 shrink-0 animate-spin" />
          ) : (
            <Brain className="size-3.5 shrink-0" />
          )}
          {block.streaming ? (
            <MarqueeText text={t("transcript.thinking")} active className="flex-none w-fit text-[13px]" />
          ) : (
            <span className="text-[13px]">{t("transcript.thought")}</span>
          )}
          {expanded ? (
            <ChevronDown className="size-3.5 opacity-60" />
          ) : (
            <ChevronRight className="size-3.5 opacity-60" />
          )}
        </>
      }
    >
      <ThoughtBody text={block.text} streaming={block.streaming} />
    </Disclosure>
  )
}

/** Live thoughts follow the newest tokens inside a 10-line window. A finished
 *  thought that the user opens starts at the top, because they came back to
 *  read it, not to watch it grow. */
function ThoughtBody({ text, streaming }: { text: string; streaming?: boolean }) {
  const t = useT()
  const scrollerRef = useRef<HTMLDivElement>(null)
  const pinnedRef = useRef(Boolean(streaming))
  const followRaf = useRef(0)
  const [fadeTop, setFadeTop] = useState(false)

  const sync = useCallback(() => {
    const el = scrollerRef.current
    if (!el) return
    setFadeTop(thoughtHasHiddenPrefix(el.scrollTop))
    pinnedRef.current = thoughtFollowsStream(el.scrollHeight, el.scrollTop, el.clientHeight)
  }, [])

  useEffect(() => {
    const el = scrollerRef.current
    if (!el) return
    el.addEventListener("scroll", sync, { passive: true })
    sync()
    return () => el.removeEventListener("scroll", sync)
  }, [sync])

  useEffect(() => {
    if (streaming) pinnedRef.current = true
  }, [streaming])

  useEffect(() => {
    if (!pinnedRef.current) return
    const el = scrollerRef.current
    if (!el) return
    followRaf.current = window.requestAnimationFrame(() => {
      if (!pinnedRef.current) return
      el.scrollTop = el.scrollHeight
      sync()
    })
    return () => window.cancelAnimationFrame(followRaf.current)
  }, [text, streaming, sync])

  return (
    <div className="relative isolate">
      {fadeTop ? (
        <div
          aria-hidden
          data-testid="thought-fade"
          className="thought-fade pointer-events-none absolute inset-x-0 top-0 z-[1] h-10"
        />
      ) : null}
      <div
        ref={scrollerRef}
        data-testid="thought-scroll"
        data-overflow-top={fadeTop ? "true" : undefined}
        role="region"
        aria-label={streaming ? t("transcript.thinking") : t("transcript.thought")}
        className="thought-scroll thin-scrollbar border-l-2 border-border pl-3"
      >
        <p className="stream-text text-[13px] leading-6 text-muted-foreground">{text}</p>
      </div>
    </div>
  )
}
