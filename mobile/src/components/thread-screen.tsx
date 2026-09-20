import { useLayoutEffect, useRef, useState } from "react"
import { ChevronLeft } from "lucide-react"

import { AskCard } from "@/components/ask-card"
import { renderBlock } from "@/components/thread-blocks"
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
  onBack,
  onOlder,
  onSend,
  onSteer,
  onStop,
  onAnswer,
  onAnswerStructured,
}: {
  detail: ThreadDetail
  blocks: CompactBlock[]
  hasMore?: boolean
  loadingOlder?: boolean
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
}) {
  const ask = pendingAsk(blocks)
  const asking = Boolean(detail.running?.ask_user || ask?.pending)
  const running = Boolean(detail.running)
  const scroller = useRef<HTMLOListElement>(null)
  const stick = useRef(true)
  const pinHeight = useRef<number | null>(null)

  const loadOlder = () => {
    if (!onOlder || loadingOlder || !hasMore) return
    pinHeight.current = scroller.current?.scrollHeight ?? 0
    onOlder()
  }

  useLayoutEffect(() => {
    const el = scroller.current
    if (!el) return
    if (loadingOlder) return
    if (pinHeight.current != null) {
      el.scrollTop = el.scrollHeight - pinHeight.current
      pinHeight.current = null
      if (el.scrollTop < 48 && hasMore) loadOlder()
      return
    }
    if (stick.current) el.scrollTop = el.scrollHeight
  }, [blocks, loadingOlder, hasMore])

  return (
    <main className="mx-auto flex h-[100dvh] max-w-lg flex-col overflow-hidden bg-background">
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
          {running ? (
            <span
              className="size-1.5 shrink-0 rounded-full bg-[hsl(var(--running))]"
              aria-hidden
            />
          ) : null}
          <h1 className="min-w-0 truncate text-sm font-medium">{detail.title}</h1>
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
      {detail.goal_on && detail.goal ? (
        <p className="truncate px-4 py-1 text-xs text-muted-foreground">{detail.goal}</p>
      ) : null}
      {detail.plan_on ? (
        <p className="px-4 text-[11px] text-[hsl(var(--running))]">{t("thread.plan")}</p>
      ) : null}

      <ol
        ref={scroller}
        className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-3 py-3"
        onScroll={(e) => {
          const el = e.currentTarget
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48
          if (el.scrollTop < 48 && el.scrollHeight > el.clientHeight + 24) loadOlder()
        }}
      >
        {hasMore ? (
          <li className="flex justify-center">
            <Button
              type="button"
              variant="ghost"
              className="h-8 text-xs text-muted-foreground"
              disabled={loadingOlder}
              onClick={loadOlder}
            >
              {loadingOlder ? t("thread.loading") : t("thread.earlier")}
            </Button>
          </li>
        ) : null}
        {blocks.map((b) => {
          const node = renderBlock(b)
          if (!node) return null
          return <li key={b.id}>{node}</li>
        })}
        {ask?.pending && (ask.questions?.length ?? 0) > 0 ? (
          <li>
            <AskCard
              questions={ask.questions!}
              onSubmit={(answers) => onAnswerStructured(ask.callId || "", answers)}
            />
          </li>
        ) : null}
      </ol>

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
      className="flex shrink-0 items-end gap-2 border-t border-border bg-background px-3 py-2"
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
