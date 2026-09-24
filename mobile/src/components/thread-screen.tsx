import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { ArrowDown, ChevronLeft, Loader2, MessageSquarePlus } from "lucide-react"

import { AskCard } from "@/components/ask-card"
import { Composer, type ComposerExtra } from "@/components/composer"
import { GoalBanner, ScheduleBanner } from "@/components/status-banners"
import { ThreadLog } from "@/components/thread-blocks"
import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import { normalizeSelectedText } from "@/lib/quote"
import type { CompactBlock } from "@/lib/transcript"
import { pendingAsk } from "@/lib/transcript"
import type { FollowupView, ModelChoice, ThreadDetail } from "@/lib/rpc"
import { cn } from "@/lib/cn"

export function ThreadScreen({
  detail,
  blocks,
  hasMore,
  loadingOlder,
  caughtUp = true,
  onBack,
  onOlder,
  followups = [],
  onSend,
  onSteer,
  onSteerFollowup,
  onDropFollowup,
  onInterrupt,
  composerPending,
  models,
  reasoningLevels,
  catalogBusy,
  onTune,
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
  followups?: FollowupView[]
  onSend: (text: string, extra?: ComposerExtra) => void
  onSteer: (text: string, extra?: ComposerExtra) => void
  onSteerFollowup?: (id: string) => void
  onDropFollowup?: (id: string) => void
  onInterrupt?: (id: string) => void
  composerPending?: boolean
  models?: ModelChoice[]
  reasoningLevels?: string[]
  catalogBusy?: boolean
  onTune?: (next: { providerId: string; model: string; reasoning: string }) => void
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
  const opening = !caughtUp && blocks.length === 0
  const asking = Boolean(detail.running?.ask_user || ask?.pending)
  const running = Boolean(detail.running)
  const waiting = Boolean(detail.waiting && !running)
  const scroller = useRef<HTMLDivElement>(null)
  const quoteSource = useRef<HTMLDivElement>(null)
  const [selectedText, setSelectedText] = useState("")
  const [quotes, setQuotes] = useState<string[]>([])
  const stick = useRef(true)
  const pinHeight = useRef<number | null>(null)
  const [atTail, setAtTail] = useState(false)
  const [behind, setBehind] = useState(false)

  useEffect(() => {
    if (asking) return
    const read = () => {
      const selection = window.getSelection()
      if (!selection || selection.isCollapsed || selection.rangeCount === 0) return
      const range = selection.getRangeAt(0)
      const source = quoteSource.current
      const inMessage = (node: Node) => {
        const element = node instanceof Element ? node : node.parentElement
        return element?.closest("[data-quote-text]")
      }
      if (!source?.contains(range.startContainer) || !source.contains(range.endContainer) ||
        !inMessage(range.startContainer) || !inMessage(range.endContainer)) {
        setSelectedText("")
        return
      }
      setSelectedText(normalizeSelectedText(selection.toString()))
      stick.current = false
    }
    document.addEventListener("selectionchange", read)
    return () => document.removeEventListener("selectionchange", read)
  }, [asking])

  const loadOlder = () => {
    if (!onOlder || loadingOlder || !hasMore || !caughtUp) return
    // The reader is already at the top, asking for what is above. Pinning
    // the old offset hides that page and leaves the last turn on screen.
    pinHeight.current = 0
    onOlder()
  }

  const pullY = useRef<number | null>(null)

  useLayoutEffect(() => {
    const el = scroller.current
    if (!el || !caughtUp) return
    el.style.overflowAnchor = "none"
    if (loadingOlder) return
    if (pinHeight.current != null) {
      // Stay at the top. A follow-up request here never leaves 加载中:
      // the new rows keep the scroller under the 48px tripwire.
      el.scrollTop = 0
      pinHeight.current = null
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
            {loadingOlder || !caughtUp ? (
              <span className="inline-flex items-center gap-1.5">
                <Loader2 className="size-3.5 motion-safe:animate-spin motion-reduce:animate-none" aria-hidden />
                {t("thread.loading")}
              </span>
            ) : (
              t("thread.earlier")
            )}
          </Button>
        </div>
      ) : null}

      {opening ? (
        <div
          role="status"
          aria-busy="true"
          className="flex min-h-0 flex-1 flex-col justify-end gap-3 px-4 py-6"
        >
          <span className="sr-only">{t("thread.loading")}</span>
          <div className="h-4 w-2/3 animate-pulse rounded-full bg-muted motion-reduce:animate-none" />
          <div className="h-4 w-1/2 animate-pulse rounded-full bg-muted motion-reduce:animate-none" />
          <div className="h-16 animate-pulse rounded-2xl bg-muted motion-reduce:animate-none" />
        </div>
      ) : (
      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col">
        {/* Android WebView will not scroll a flex-column <ol>. Bounded
            overflow box; Earlier sits above so paging is not a dead drag. */}
        <div
          ref={scroller}
          data-testid="transcript"
          className={cn(
            "min-h-0 min-w-0 w-full flex-1 overflow-x-hidden overflow-y-scroll overscroll-y-contain touch-pan-y px-3 py-2 [-webkit-overflow-scrolling:touch]",
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
          }}
          onTouchStart={(e) => onPullStart(e.touches[0]?.clientY ?? 0)}
          onTouchMove={(e) => onPullMove(e.touches[0]?.clientY ?? 0)}
          onTouchEnd={() => {
            pullY.current = null
          }}
        >
          {/* Bottom-aligned: a short conversation sits above the composer
              instead of floating under a screen of blank. */}
          <div className="flex min-h-full min-w-0 w-full flex-col justify-end gap-1.5">
            <div ref={quoteSource} data-quote-source="">
              <ThreadLog blocks={blocks} running={running} />
            </div>
            {ask?.pending && (ask.questions?.length ?? 0) > 0 ? (
              <AskCard
                questions={ask.questions!}
                disabled={composerPending}
                onSubmit={(answers) => onAnswerStructured(ask.callId || "", answers)}
              />
            ) : null}
          </div>
        </div>
        {selectedText && !asking ? (
          <Button
            type="button"
            variant="outline"
            className="absolute bottom-3 right-3 z-10 h-9 gap-1.5 rounded-full border border-border px-3 shadow-md"
            onClick={() => {
              setQuotes((current) => [...current, selectedText])
              setSelectedText("")
              window.getSelection()?.removeAllRanges()
            }}
          >
            <MessageSquarePlus className="size-4" aria-hidden />
            {t("quote.add")}
          </Button>
        ) : null}
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
      )}

      {followups.length > 0 ? (
        <QueueTray
          items={followups}
          onSteer={onSteerFollowup}
          onDrop={onDropFollowup}
          onInterrupt={running ? onInterrupt : undefined}
        />
      ) : null}
      <Composer
        quotes={asking ? [] : quotes}
        onQuotesChange={setQuotes}
        // The box and the button cannot share a name, or a screen reader
        // announces two "Answer" controls and a test cannot pick either.
        label={asking ? t("thread.answerBox") : t("thread.message")}
        sendLabel={asking ? t("thread.answer") : running ? t("thread.followUp") : t("thread.send")}
        hint={running && !asking ? t("thread.sendHint") : undefined}
        steerLabel={running && !asking ? t("thread.steer") : undefined}
        onSteer={running && !asking ? onSteer : undefined}
        pending={composerPending}
        models={models}
        reasoningLevels={reasoningLevels}
        providerId={detail.provider_id}
        model={detail.model}
        reasoning={detail.reasoning ?? ""}
        catalogBusy={catalogBusy}
        onTune={onTune}
        onSubmit={asking ? onAnswer : onSend}
      />
    </main>
  )
}

function QueueTray({
  items,
  onSteer,
  onDrop,
  onInterrupt,
}: {
  items: FollowupView[]
  onSteer?: (id: string) => void
  onDrop?: (id: string) => void
  onInterrupt?: (id: string) => void
}) {
  return (
    <div data-testid="followup-queue" className="border-t border-border px-3 py-2">
      <div className="mb-1 flex items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground">{t("queue.waiting")}</p>
        {onInterrupt ? (
          <Button variant="ghost" className="h-8 px-2" onClick={() => onInterrupt(items[0].id)}>
            {t("queue.interrupt")}
          </Button>
        ) : null}
      </div>
      <ul className="flex flex-col gap-1">
        {items.map((item) => (
          <li key={item.id} className="flex items-center gap-2">
            <p className="min-w-0 flex-1 truncate text-sm">{item.text}</p>
            <Button
              variant="ghost"
              className="h-8 shrink-0 px-2"
              aria-label={t("queue.steer")}
              onClick={() => onSteer?.(item.id)}
            >
              {t("queue.steer")}
            </Button>
            <Button
              variant="ghost"
              className="h-8 shrink-0 px-2 text-muted-foreground"
              aria-label={t("queue.remove")}
              onClick={() => onDrop?.(item.id)}
            >
              {t("queue.remove")}
            </Button>
          </li>
        ))}
      </ul>
    </div>
  )
}
