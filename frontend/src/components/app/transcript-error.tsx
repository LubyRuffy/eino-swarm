import { AlertTriangle } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  isOutputBudgetError,
  MAX_COMPLETION_SETTINGS_KEY,
  type BudgetRetryMessage,
} from "@/lib/output-budget"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { openSettings, useSettingsSheet } from "@/store/settings-sheet"

/** A failed turn. The output-budget sentence is the one a desktop client
 *  can fix in Settings, so that row grows a hint and a link. The retry
 *  stays hidden until that sheet closes: the new cap is what the next
 *  run should use. Other errors stay the server text. */
export function TranscriptError({
  text,
  turnId,
  retry,
  onResend,
}: {
  text: string
  turnId?: string
  retry?: BudgetRetryMessage
  onResend?: (text: string, seq: number) => void
}) {
  const t = useT()
  const budget = isOutputBudgetError(text)
  const activeId = useApp((s) => s.activeId)
  const running = useApp((s) => s.status.running || s.transcript.running)
  const back = useSettingsSheet((s) => s.budgetReturn)
  const ready =
    Boolean(budget && retry && onResend) &&
    back?.phase === "back" &&
    back.threadId === activeId &&
    back.turnId === turnId
  return (
    <div
      className="my-2 flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
      data-testid={budget ? "output-budget-error" : undefined}
    >
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">
        <p className="stream-text">{text}</p>
        {budget ? (
          <div className="mt-1">
            <p>
              {t("transcript.outputBudgetHint")}{" "}
              <Button
                type="button"
                variant="link"
                className="h-auto px-0 py-0 text-sm text-destructive underline underline-offset-4"
                onClick={() =>
                  openSettings("swarm", MAX_COMPLETION_SETTINGS_KEY, {
                    threadId: useApp.getState().activeId ?? "",
                    turnId: turnId ?? "",
                  })
                }
              >
                {t("transcript.outputBudgetSettings")}
              </Button>
            </p>
            {ready && retry && onResend ? (
              <Button
                type="button"
                size="sm"
                className="mt-2"
                disabled={running}
                data-testid="output-budget-retry"
                onClick={() => onResend(retry.text, retry.seq)}
              >
                {t("transcript.outputBudgetRetry")}
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
}
