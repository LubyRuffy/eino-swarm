import { WorkerModelRecovery } from "./worker-model-recovery"

import { AlertTriangle, RotateCcw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { localizeNotice } from "@/lib/i18n"
import { firstByteTimeoutDuration, firstByteTimeoutSettingsKey } from "@/lib/first-byte-timeout"
import {
  isOutputBudgetError,
  MAX_COMPLETION_SETTINGS_KEY,
} from "@/lib/output-budget"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { openSettings, useSettingsSheet } from "@/store/settings-sheet"

/** Clicking Retry continues the failed turn from its stored context. */
export function TranscriptError({
  text,
  turnId,
  onRetry,
  retrying = false,
}: {
  text: string
  turnId?: string
  onRetry?: () => void
  retrying?: boolean
}) {
  const t = useT()
  const budget = isOutputBudgetError(text)
  const firstByte = firstByteTimeoutDuration(text)
  const providerId = useApp((s) => s.turns.find((turn) => turn.id === turnId)?.provider_id)
  const running = useApp((s) => s.status.running || s.transcript.running)
  const latest = useApp((s) => s.transcript.turns.at(-1))
  const unavailable = useApp((s) => !s.loaded || !s.activeId || s.status.awaiting_answer || s.meta?.startup === "restoring")
  const settingsOpen = useSettingsSheet((s) => s.open)
  const retryable = onRetry && turnId && latest?.id === turnId && latest.status === "error"
  return (
    <div
      className="my-2 flex items-start gap-2 rounded-lg border border-destructive/40 bg-destructive/10 px-3 py-2 text-(length:--chrome-font-size) text-destructive"
      data-testid={budget ? "output-budget-error" : firstByte ? "first-byte-timeout-error" : undefined}
    >
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">
        <p className="stream-text">{localizeNotice(text, t.locale)}</p>
        {firstByte ? (
          <div className="mt-1">
            <p>{t("transcript.firstByteTimeoutHint")}</p>
            <Button
              type="button"
              variant="link"
              className="h-auto px-0 py-0 text-(length:--chrome-font-size) text-destructive underline underline-offset-4"
              onClick={() => openSettings("models", firstByteTimeoutSettingsKey(providerId ?? ""))}
            >
              {t("transcript.firstByteTimeoutSettings")}
            </Button>
          </div>
        ) : null}
        {budget ? (
          <div className="mt-1">
            <p>
              {t("transcript.outputBudgetHint")}{" "}
              <Button
                type="button"
                variant="link"
                className="h-auto px-0 py-0 text-(length:--chrome-font-size) text-destructive underline underline-offset-4"
                onClick={() =>
                  openSettings("swarm", MAX_COMPLETION_SETTINGS_KEY)
                }
              >
                {t("transcript.outputBudgetSettings")}
              </Button>
            </p>
          </div>
        ) : null}
        {/restore worker (configuration|model).*provider: unknown provider/.test(text) ? <WorkerModelRecovery key={turnId} turnId={turnId} /> : null}
        {retryable ? (
          <Button
            type="button"
            size="sm"
            className="mt-2"
            disabled={running || retrying || unavailable || settingsOpen}
            data-testid="transcript-error-retry"
            onClick={onRetry}
          >
            <RotateCcw />
            {t("transcript.outputBudgetRetry")}
          </Button>
        ) : null}
      </div>
    </div>
  )
}
