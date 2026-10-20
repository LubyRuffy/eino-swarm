import { useState } from "react"
import { Sparkles } from "lucide-react"

import { Button } from "@/components/ui/button"
import { reviewNotice, reviewPanelHint } from "@/lib/transcript-review"
import type { ReviewOutcome } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { useTraceFocus } from "@/store/trace-focus"

/** Diagnostics come from bounded metadata. Never reflect raw proposals,
 *  legacy reasons or upstream errors in a user-facing status. */
export function ReviewStatus({ outcome, turnId, panel = false }: {
  outcome: ReviewOutcome
  turnId?: string
  panel?: boolean
}) {
  const t = useT()
  const [opening, setOpening] = useState(false)
  const [traceError, setTraceError] = useState(false)
  const openTrace = async () => {
    if (!turnId) return
    setOpening(true)
    setTraceError(false)
    const app = useApp.getState()
    const threadId = app.activeId
    try {
      // Older review events can outlive the currently loaded transcript tail.
      // Load their source turn before selecting it in the existing Trace tab.
      if (threadId && !await app.loadUntilTurn(turnId)) { setTraceError(true); return }
      if (threadId !== useApp.getState().activeId) return
      useTraceFocus.getState().open(turnId)
    } catch {
      setTraceError(true)
    } finally {
      setOpening(false)
    }
  }
  const text = panel ? reviewPanelHint(outcome, t.locale) : reviewNotice(outcome, t.locale)
  if (!text) return null
  return (
    <div
      role={panel ? "status" : undefined}
      data-testid={panel ? "review-status" : "memory-notice"}
      className={panel ? "text-xs text-muted-foreground" : "my-2 flex items-start gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground"}
    >
      {!panel ? <Sparkles className="mt-0.5 size-3.5 shrink-0" /> : null}
      <div className="min-w-0 flex-1">
        <p className="stream-text whitespace-pre-wrap">{text}</p>
        {turnId && (outcome.err || (outcome.stop_reason && outcome.stop_reason !== "completed")) ? (
          <Button
            type="button"
            size="sm"
            variant="link"
            className="h-auto px-0 text-xs"
            disabled={opening}
            aria-busy={opening}
            onClick={() => void openTrace()}
            aria-label={`${t("panel.trace")} ${turnId}`}
          >
            {t("panel.trace")} · {turnId}
          </Button>
        ) : null}
        {traceError ? <p role="alert">{t("panel.noTrace")}</p> : null}
      </div>
    </div>
  )
}
