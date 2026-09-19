import { useState } from "react"

import { AskCard } from "@/components/ask-card"
import { PhoneMarkdown } from "@/components/markdown"
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
  onBack,
  onSend,
  onSteer,
  onStop,
  onAnswer,
  onAnswerStructured,
}: {
  detail: ThreadDetail
  blocks: CompactBlock[]
  onBack: () => void
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

  return (
    <main className="mx-auto flex min-h-[100dvh] max-w-lg flex-col">
      <header className="flex items-center gap-2 px-2 py-2">
        <Button variant="ghost" onClick={onBack}>
          {t("thread.back")}
        </Button>
        <h1 className="min-w-0 flex-1 truncate text-lg font-semibold">{detail.title}</h1>
        {running ? (
          <Button variant="destructive" onClick={onStop}>
            {t("thread.stop")}
          </Button>
        ) : null}
      </header>
      {detail.goal_on && detail.goal ? (
        <p className="px-4 text-sm text-muted-foreground">{detail.goal}</p>
      ) : null}
      {detail.plan_on ? (
        <p className="px-4 text-xs text-[hsl(var(--running))]">{t("thread.plan")}</p>
      ) : null}

      <ol className="flex flex-1 flex-col gap-3 overflow-y-auto px-4 py-3">
        {blocks.map((b) => (
          <li key={b.id}>{renderBlock(b)}</li>
        ))}
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

function renderBlock(b: CompactBlock) {
  if (b.kind === "user") {
    return (
      <div className="ml-8 rounded-2xl bg-primary px-3 py-2 text-sm text-primary-foreground">
        <p className="whitespace-pre-wrap">{b.text}</p>
        {b.hasImages ? (
          <p className="mt-1 text-xs opacity-80">{t("thread.image")}</p>
        ) : null}
      </div>
    )
  }
  if (b.kind === "steer") {
    return (
      <div className="ml-8 rounded-2xl bg-accent px-3 py-2 text-sm">
        <p className="whitespace-pre-wrap">{b.text}</p>
      </div>
    )
  }
  if (b.kind === "answer") {
    return (
      <div className={cn("mr-6", b.streaming && "opacity-90")}>
        <PhoneMarkdown text={b.text} />
      </div>
    )
  }
  if (b.kind === "tool") {
    return (
      <p className="text-xs text-muted-foreground">
        {b.pending ? (
          <span className="text-[hsl(var(--running))]">{b.toolName || b.text}</span>
        ) : (
          b.toolName || b.text
        )}
      </p>
    )
  }
  if (b.kind === "spawn") {
    return <p className="text-xs text-muted-foreground">{b.text}</p>
  }
  if (b.kind === "error") {
    return <p className="text-sm text-destructive">{b.text}</p>
  }
  if (b.kind === "notice") {
    return <p className="text-xs text-muted-foreground">{b.text}</p>
  }
  return null
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
      className="sticky bottom-0 flex flex-col gap-2 border-t border-border bg-background px-4 py-3"
      onSubmit={(e) => {
        e.preventDefault()
        submit(asking ? "answer" : "send")
      }}
    >
      <Textarea
        aria-label={asking ? t("thread.answer") : t("thread.message")}
        value={text}
        onChange={(e) => setText(e.target.value)}
        rows={2}
      />
      <div className="flex gap-2">
        <Button type="submit">{label}</Button>
        {running ? (
          <Button type="button" variant="outline" onClick={() => submit("steer")}>
            {t("thread.steer")}
          </Button>
        ) : null}
      </div>
    </form>
  )
}
