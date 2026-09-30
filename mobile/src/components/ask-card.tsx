import { useRef, useState } from "react"

import { ASK_OTHER_ID, type AskQuestion } from "@/lib/ask"
import { t } from "@/lib/i18n"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/input"
import { cn } from "@/lib/cn"

export function AskCard({
  questions,
  disabled,
  onSubmit,
}: {
  questions: AskQuestion[]
  disabled?: boolean
  onSubmit: (answers: Record<string, { answers: string[] }>) => void | Promise<void>
}) {
  const [picks, setPicks] = useState<Record<string, string>>({})
  const [other, setOther] = useState<Record<string, string>>({})
  const [error, setError] = useState("")
  const [submitting, setSubmitting] = useState(false)
  const [submitted, setSubmitted] = useState(false)
  const inFlight = useRef(false)

  const resolve = () => {
    const out: Record<string, { answers: string[] }> = {}
    for (const q of questions) {
      const optId = picks[q.id]
      if (!optId) return null
      if (optId === ASK_OTHER_ID) {
        const text = (other[q.id] ?? "").trim()
        if (!text) return null
        out[q.id] = { answers: [text] }
        continue
      }
      const label = q.options.find((o) => o.id === optId)?.label
      if (!label) return null
      out[q.id] = { answers: [label] }
    }
    return out
  }

  const ready = resolve() !== null

  return (
    <div className="rounded-2xl border border-border bg-card p-4" data-testid="ask-card">
      <p className="text-sm text-muted-foreground">{t("ask.title")}</p>
      <form
        className="mt-3 flex flex-col gap-4"
        onSubmit={async (e) => {
          e.preventDefault()
          if (disabled || inFlight.current || submitted) return
          const all = resolve()
          if (!all) return
          inFlight.current = true
          setSubmitting(true)
          setError("")
          try {
            await onSubmit(all)
            setSubmitted(true)
          } catch (cause) {
            setError(cause instanceof Error ? cause.message : t("ask.failed"))
          } finally {
            inFlight.current = false
            setSubmitting(false)
          }
        }}
      >
        {questions.map((q) => (
          <fieldset key={q.id} className="flex flex-col gap-2">
            {q.header ? (
              <legend className="text-xs text-muted-foreground">{q.header}</legend>
            ) : null}
            <p className="text-sm font-medium">{q.prompt}</p>
            <div className="flex flex-col gap-1">
              {q.options.map((o) => (
                <button
                  key={o.id}
                  type="button"
                  disabled={disabled || submitting || submitted}
                  className={cn(
                    "rounded-md border border-border px-3 py-2 text-left text-sm",
                    picks[q.id] === o.id && "bg-accent",
                  )}
                  onClick={() => setPicks((p) => ({ ...p, [q.id]: o.id }))}
                >
                  {o.id === ASK_OTHER_ID ? t("ask.other") : o.label}
                </button>
              ))}
            </div>
            {picks[q.id] === ASK_OTHER_ID ? (
              <Textarea
                aria-label={t("ask.other")}
                disabled={disabled || submitting || submitted}
                value={other[q.id] ?? ""}
                onChange={(e) =>
                  setOther((prev) => ({ ...prev, [q.id]: e.target.value }))
                }
              />
            ) : null}
          </fieldset>
        ))}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {submitted ? <p role="status" className="text-sm text-muted-foreground">{t("ask.submitted")}</p> : null}
        <Button type="submit" disabled={disabled || !ready || submitting || submitted} data-testid="ask-submit">
          {submitting ? t("ask.submitting") : t("ask.submit")}
        </Button>
      </form>
    </div>
  )
}
