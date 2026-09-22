import { useLayoutEffect, useRef, useState } from "react"
import { ChevronLeft } from "lucide-react"

import { AskCard } from "@/components/ask-card"
import { GoalBanner, ScheduleBanner } from "@/components/status-banners"
import { ThreadLog } from "@/components/thread-blocks"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/input"
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
    if (stick.current) el.scrollTop = el.scrollHeight
    if (!atTail) setAtTail(true)
  }, [caughtUp, blocks, loadingOlder, hasMore, atTail])

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
              className="size-1.5 shrink-0 rounded-full bg-[hsl(var(--running))]"
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
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48
          if (!atTail) return
          if (el.scrollTop < 48) loadOlder()
        }}
        onTouchStart={(e) => onPullStart(e.touches[0]?.clientY ?? 0)}
        onTouchMove={(e) => onPullMove(e.touches[0]?.clientY ?? 0)}
        onTouchEnd={() => {
          pullY.current = null
        }}
      >
        <div className="flex min-w-0 w-full flex-col gap-2">
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

      <Composer
        asking={asking}
        running={running && !asking}
        onSend={onSend}
        onSteer={onSteer}
        onAnswer={onAnswer}
      />
    </main>
  )
}

function Composer({
  asking,
  running,
  onSend,
  onSteer,
  onAnswer,
}: {
  asking: boolean
  running: boolean
  onSend: (text: string) => void
  onSteer: (text: string) => void
  onAnswer: (text: string) => void
}) {
  const [text, setText] = useState("")
  const submit = (mode: "send" | "steer" | "answer") => {
    const v = text.trim()
    if (!v) return
    if (mode === "steer") onSteer(v)
    else if (mode === "answer") onAnswer(v)
    else onSend(v)
    setText("")
  }
  const label = asking
    ? t("thread.answer")
    : running
      ? t("thread.followUp")
      : t("thread.send")
  return (
    <form
      className="flex min-w-0 shrink-0 items-end gap-2 border-t border-border bg-background px-3 py-2"
      onSubmit={(e) => {
        e.preventDefault()
        submit(asking ? "answer" : "send")
      }}
    >
      <Textarea
        aria-label={asking ? t("thread.answer") : t("thread.message")}
        value={text}
        onChange={(e) => setText(e.target.value)}
        rows={1}
        className="min-h-10 max-h-24 flex-1 resize-none rounded-2xl border-0 bg-muted px-3 py-2"
      />
      {running ? (
        <Button
          type="button"
          variant="ghost"
          className="h-10 shrink-0 px-3"
          onClick={() => submit("steer")}
        >
          {t("thread.steer")}
        </Button>
      ) : null}
      <Button type="submit" className={cn("h-10 shrink-0 rounded-2xl px-4")}>
        {label}
      </Button>
    </form>
  )
}
