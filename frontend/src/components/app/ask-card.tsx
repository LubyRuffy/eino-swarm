import { useState } from "react"

import { AskMark } from "@/components/app/ask-mark"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { ASK_OTHER_ID, type AskCard, type AskQuestion } from "@/lib/transcript-ask"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

/** One ask_user call, styled as a Codex-style question dialog in the transcript. */
export function AskCardView({ card }: { card: AskCard }) {
  const t = useT()
  const answerAsk = useApp((s) => s.answerAsk)
  const [picks, setPicks] = useState<Record<string, string>>({})
  const [other, setOther] = useState<Record<string, string>>({})
  const [busy, setBusy] = useState(false)

  const send = async (answers: Record<string, string>) => {
    if (!card.pending || busy) return
    setBusy(true)
    try {
      const body: Record<string, { answers: string[] }> = {}
      for (const [id, text] of Object.entries(answers)) {
        body[id] = { answers: [text] }
      }
      await answerAsk(card.callId, body)
    } finally {
      setBusy(false)
    }
  }

  const resolve = (nextPicks = picks, nextOther = other) => {
    const out: Record<string, string> = {}
    for (const q of card.questions) {
      const optId = nextPicks[q.id]
      if (!optId) return null
      if (optId === ASK_OTHER_ID) {
        const text = (nextOther[q.id] ?? "").trim()
        if (!text) return null
        out[q.id] = text
        continue
      }
      const label = q.options.find((o) => o.id === optId)?.label
      if (!label) return null
      out[q.id] = label
    }
    return out
  }

  const pick = (questionId: string, optionId: string) => {
    const next = { ...picks, [questionId]: optionId }
    setPicks(next)
    if (optionId !== ASK_OTHER_ID) {
      setOther((prev) => ({ ...prev, [questionId]: "" }))
    }
  }

  const submit = () => {
    const all = resolve()
    if (all) void send(all)
  }

  const ready = resolve() !== null

  return (
    <div
      data-testid="ask-card"
      className={cn(
        "relative my-3 w-full rounded-2xl border bg-card p-4 shadow-md",
        card.pending && "border-ask",
      )}
    >
      <div className="flex items-center gap-2">
        {card.pending ? <AskMark className="size-3.5" pulse={false} /> : null}
        <p
          className={cn(
            "text-sm",
            card.pending ? "font-medium text-ask" : "text-muted-foreground",
          )}
        >
          {card.pending ? t("ask.needsYou") : t("ask.title")}
        </p>
      </div>
      {card.pending ? (
        <p className="mt-1 text-xs text-muted-foreground">{t("ask.hint")}</p>
      ) : null}
      {card.pending ? (
        <form
          className="mt-3 flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          {card.questions.map((q) => (
            <AskQuestionBlock
              key={q.id}
              question={q}
              pickId={picks[q.id]}
              otherText={other[q.id] ?? ""}
              busy={busy}
              onPick={(optionId) => pick(q.id, optionId)}
              onOther={(text) =>
                setOther((prev) => ({ ...prev, [q.id]: text }))
              }
            />
          ))}
          <div className="flex justify-end">
            <Button
              type="submit"
              disabled={busy || !ready}
              data-testid="ask-submit"
            >
              {t("ask.submit")}
            </Button>
          </div>
        </form>
      ) : (
        <div className="mt-3 flex flex-col gap-3">
          {card.questions.map((q) => (
            <div key={q.id} className="rounded-xl border bg-background px-4 py-3">
              {q.header ? (
                <p className="text-xs text-muted-foreground">{q.header}</p>
              ) : null}
              <p className="text-sm font-medium">{q.prompt}</p>
              <p className="mt-2 text-sm text-muted-foreground">
                {card.answers?.[q.id] ?? ""}
              </p>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function AskQuestionBlock({
  question,
  pickId,
  otherText,
  busy,
  onPick,
  onOther,
}: {
  question: AskQuestion
  pickId?: string
  otherText: string
  busy: boolean
  onPick: (optionId: string) => void
  onOther: (text: string) => void
}) {
  const t = useT()
  const promptId = `ask-prompt-${question.id}`
  const otherOn = pickId === ASK_OTHER_ID

  return (
    <div className="rounded-xl border bg-background px-2 py-3">
      {question.header ? (
        <p className="mb-1 px-2 text-xs text-muted-foreground">{question.header}</p>
      ) : null}
      <p id={promptId} className="px-2 text-sm font-medium leading-snug">
        {question.prompt}
      </p>
      <div
        role="radiogroup"
        aria-labelledby={promptId}
        className="mt-2 flex flex-col"
        onKeyDown={(e) => {
          if (e.target instanceof HTMLTextAreaElement) return
          const n = Number(e.key)
          if (n < 1 || n > question.options.length) return
          e.preventDefault()
          onPick(question.options[n - 1].id)
        }}
      >
        {question.options.map((o, i) => {
          const selected = pickId === o.id
          const label = o.id === ASK_OTHER_ID ? t("ask.other") : o.label
          return (
            <button
              key={o.id}
              type="button"
              role="radio"
              aria-checked={selected}
              disabled={busy}
              data-testid={`ask-option-${o.id}`}
              aria-label={label}
              className={cn(
                "flex items-center gap-3 rounded-lg px-2 py-2.5 text-left text-sm transition-colors",
                "hover:bg-muted hover:text-accent-foreground",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                selected && "bg-muted text-accent-foreground",
              )}
              onClick={() => onPick(o.id)}
            >
              <span className="w-5 shrink-0 text-sm tabular-nums text-muted-foreground">
                {i + 1}.
              </span>
              <span className="min-w-0 flex-1">
                <span className="block leading-snug">{label}</span>
                {o.description ? (
                  <span className="mt-0.5 block text-xs text-muted-foreground">
                    {o.description}
                  </span>
                ) : null}
              </span>
              <span
                aria-hidden
                className={cn(
                  "flex size-4 shrink-0 items-center justify-center rounded-full border",
                  selected
                    ? "border-primary"
                    : "border-muted-foreground/40",
                )}
              >
                {selected ? (
                  <span className="size-2 rounded-full bg-primary" />
                ) : null}
              </span>
            </button>
          )
        })}
      </div>
      {otherOn ? (
        <Textarea
          id={`ask-other-${question.id}`}
          data-testid={`ask-other-${question.id}`}
          data-ask-other="true"
          aria-label={t("ask.other")}
          rows={2}
          className="mt-2 min-h-12"
          placeholder={t("ask.otherPlaceholder")}
          value={otherText}
          disabled={busy}
          onChange={(e) => onOther(e.target.value)}
        />
      ) : null}
    </div>
  )
}
