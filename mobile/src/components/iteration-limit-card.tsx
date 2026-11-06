import { useRef, useState } from "react"
import { Button } from "./ui/button"
import { t } from "@/lib/i18n"
import { iterationConfirmationText, type IterationConfirmation } from "@/lib/iteration-limit"

export function IterationLimitCard({ pause, onContinue, disabled }: {
  pause?: IterationConfirmation
  onContinue?: (proceed: boolean) => Promise<void>
  disabled?: boolean
}) {
  const pending = useRef(false)
  const [submitted, setSubmitted] = useState(false)
  const [error, setError] = useState("")
  const decide = async (proceed: boolean) => {
    if (!onContinue || disabled || pending.current) return
    pending.current = true
    setSubmitted(true)
    setError("")
    try { await onContinue(proceed) } catch (err) {
      pending.current = false
      setSubmitted(false)
      setError(err instanceof Error ? err.message : t("thread.limitFailed"))
    }
  }
  return (
    <div data-testid="iteration-limit" className="my-2 rounded-lg border border-border bg-muted/50 px-3 py-3 text-sm">
      <p className="text-muted-foreground">{pause ? iterationConfirmationText(pause) : t("thread.limitPaused")}</p>
      {onContinue ? <div className="mt-3 flex gap-2">
        <Button aria-label={t(pause ? "thread.limitContinueAria" : "thread.pausedContinueAria")} disabled={disabled || submitted} onClick={() => void decide(true)}>
          {t("thread.limitContinue")}
        </Button>
        <Button variant="outline" aria-label={t(pause ? "thread.limitStopAria" : "thread.pausedStopAria")} disabled={disabled || submitted} onClick={() => void decide(false)}>
          {t("thread.stop")}
        </Button>
      </div> : null}
      {error ? <p role="alert" className="mt-2 text-sm text-destructive">{error}</p> : null}
    </div>
  )
}
