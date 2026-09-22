import { useLayoutEffect, useRef, useState } from "react"
import { ArrowDown, ChevronLeft } from "lucide-react"

import { AskCard } from "@/components/ask-card"
import { Composer } from "@/components/composer"
import { GoalBanner, ScheduleBanner } from "@/components/status-banners"
import { ThreadLog } from "@/components/thread-blocks"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import type { CompactBlock } from "@/lib/transcript"
import { pendingAsk } from "@/lib/transcript"
import type { ThreadDetail } from "@/lib/rpc"
import { cn } from "@/lib/cn"

export function ThreadScreen({
  detail,
  blocks,
  hasMore,
  loadingOlder,
  caughtUp = true,
  onBack,
  onOlder,
  onSend,
  onSteer,
  onStop,
  onAnswer,
  onAnswerStructured,
  onRunNow,
  onCancelWait,
  onResumeGoal,
}: {
  detail: ThreadDetail
  blocks: CompactBlock[]
  hasMore?: boolean
  loadingOlder?: boolean
  caughtUp?: boolean
  onBack: () => void
  onOlder?: () => void
  onSend: (text: string) => void
  onSteer: (text: string) => void
  onStop: () => void
  onAnswer: (text: string) => void
  onAnswerStructured: (
    callId: string,
    answers: Record<string, { answers: string[] }>,
  ) => void
  onRunNow?: () => void
  onCancelWait?: () => void
  onResumeGoal?: () => void
}) {
  const ask = pendingAsk(blocks)
  const asking = Boolean(detail.running?.ask_user || ask?.pending)
  const running = Boolean(detail.running)
  const waiting = Boolean(detail.waiting && !running)
  const scroller = useRef<HTMLDivElement>(null)
  const stick = useRef(true)
  const pinHeight = useRef<number | null>(null)
  const [atTail, setAtTail] = useState(false)
  const [behind, setBehind] = useState(false)

  const loadOlder = () => {
    if (!onOlder || loadingOlder || !hasMore || !caughtUp) return
    pinHeight.current = scroller.current?.scrollHeight ?? 0
    onOlder()
  }

  const pullY = useRef<number | null>(null)

  useLayoutEffect(() => {
    const el = scroller.current
    if (!el || !caughtUp) return
    el.style.overflowAnchor = "none"
    if (loadingOlder) return
    if (pinHeight.current != null) {
      el.scrollTop = el.scrollHeight - pinHeight.current
      pinHeight.current = null
      if (el.scrollTop < 48 && hasMore) loadOlder()
      return
    }
    if (stick.current) {
      el.scrollTop = el.scrollHeight
      if (behind) setBehind(false)
    }
    if (!atTail) setAtTail(true)
  }, [caughtUp, blocks, loadingOlder, hasMore, atTail, behind])

  const toLatest = () => {
    const el = scroller.current
    if (!el) return
    stick.current = true
    setBehind(false)
    el.scrollTo({ top: el.scrollHeight, behavior: "smooth" })
  }

  const onPullStart = (y: number) => {
    pullY.current = y
  }
  const onPullMove = (y: number) => {
    const start = pullY.current
    const el = scroller.current
    if (start == null || !el || !hasMore) return
    if (el.scrollTop <= 0 && y - start > 48) {
      pullY.current = null
      loadOlder()
    }
  }

  return (
    <main className="mx-auto flex h-full min-w-0 w-full max-w-lg flex-col overflow-hidden bg-background">
      <header className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-1">
        <Button
          variant="ghost"
          className="size-10 shrink-0 px-0"
          onClick={onBack}
          aria-label={t("thread.back")}
        >
          <ChevronLeft className="size-5" />
        </Button>
        <div className="flex min-w-0 flex-1 items-center gap-2">
          {running || waiting ? (
            <span
              className="size-1.5 shrink-0 rounded-full bg-[hsl(var(--running))] motion-safe:animate-pulse"
              aria-hidden
            />
          ) : null}
          <h1 className="min-w-0 truncate text-sm font-medium">{detail.title}</h1>
          {waiting ? (
            <span
              data-testid="thread-status"
              className="shrink-0 text-xs text-[hsl(var(--running))]"
            >
              {t("thread.waiting")}
            </span>
          ) : null}
        </div>
        {running ? (
          <Button
            variant="ghost"
            className="h-8 shrink-0 px-2 text-destructive"
            onClick={onStop}
          >
            {t("thread.stop")}
          </Button>
        ) : null}
      </header>
      <GoalBanner
        detail={detail}
        running={running}
        waiting={waiting}
        onResume={onResumeGoal}
      />
      <ScheduleBanner
        detail={detail}
        running={running}
        onRunNow={onRunNow}
        onCancel={onCancelWait}
      />
      {detail.plan_on ? (
        <p className="px-4 text-[11px] text-[hsl(var(--running))]">{t("thread.plan")}</p>
      ) : null}

      {hasMore ? (
        <div className="flex shrink-0 justify-center border-b border-border">
          <Button
            type="button"
            variant="ghost"
            className="h-8 text-xs text-muted-foreground"
            disabled={loadingOlder || !caughtUp}
            onClick={loadOlder}
          >
            {loadingOlder || !caughtUp ? t("thread.loading") : t("thread.earlier")}
          </Button>
        </div>
      ) : null}

      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {/* Android WebView will not scroll a flex-column <ol>. Bounded
            overflow box; Earlier sits above so paging is not a dead drag. */}
        <div
          ref={scroller}
          data-testid="transcript"
          className={cn(
            "min-h-0 min-w-0 w-full flex-1 overflow-x-hidden overflow-y-scroll overscroll-y-contain touch-pan-y px-3 py-3 [-webkit-overflow-scrolling:touch]",
            !atTail && "invisible",
          )}
          onScroll={(e) => {
            const el = e.currentTarget
            const gap = el.scrollHeight - el.scrollTop - el.clientHeight
            stick.current = gap < 48
            // The button is for a reader who scrolled away, not for the two
            // pixels of slack a streaming answer leaves behind.
            const away = gap > 240
            if (away !== behind) setBehind(away)
            if (!atTail) return
            if (el.scrollTop < 48) loadOlder()
          }}
          onTouchStart={(e) => onPullStart(e.touches[0]?.clientY ?? 0)}
          onTouchMove={(e) => onPullMove(e.touches[0]?.clientY ?? 0)}
          onTouchEnd={() => {
            pullY.current = null
          }}
        >
          {/* Bottom-aligned: a short conversation sits above the composer
              instead of floating under a screen of blank. */}
          <div className="flex min-h-full min-w-0 w-full flex-col justify-end gap-2">
            {!caughtUp && blocks.length === 0 ? (
              <p className="px-1 text-xs text-muted-foreground">{t("thread.loading")}</p>
            ) : (
              <ThreadLog blocks={blocks} running={running} />
            )}
            {ask?.pending && (ask.questions?.length ?? 0) > 0 ? (
              <AskCard
                questions={ask.questions!}
                onSubmit={(answers) => onAnswerStructured(ask.callId || "", answers)}
              />
            ) : null}
          </div>
        </div>
        {behind && atTail ? (
          <button
            type="button"
            data-testid="to-latest"
            aria-label={t("thread.toLatest")}
            className={cn(
              "absolute bottom-3 left-1/2 flex size-9 -translate-x-1/2 items-center justify-center",
              "rounded-full border border-border bg-card text-foreground shadow-md",
              "transition-opacity active:bg-accent",
            )}
            onClick={toLatest}
          >
            <ArrowDown className="size-4" aria-hidden />
          </button>
        ) : null}
      </div>

      <Composer
        // The box and the button cannot share a name, or a screen reader
        // announces two "Answer" controls and a test cannot pick either.
        label={asking ? t("thread.answerBox") : t("thread.message")}
        sendLabel={asking ? t("thread.answer") : running ? t("thread.followUp") : t("thread.send")}
        hint={running && !asking ? t("thread.sendHint") : undefined}
        steerLabel={running && !asking ? t("thread.steer") : undefined}
        onSteer={running && !asking ? onSteer : undefined}
        onSubmit={asking ? onAnswer : onSend}
      />
    </main>
  )
}
