import { AlertTriangle } from "lucide-react"

import { Button } from "@/components/ui/button"
import { agentFailure } from "@/lib/agent-failure"
import { localizeNotice } from "@/lib/i18n"
import type { AgentState } from "@/lib/transcript"
import { useT } from "@/lib/use-t"
import { openSettings } from "@/store/settings-sheet"

/** The failure stays outside the following log so opening a long worker
 *  history cannot scroll its reason and repair entry out of view. */
export function AgentFailure({ agent }: { agent: AgentState }) {
  const t = useT()
  if (agent.status !== "failed") return null
  const error = agent.error?.trim()
  const parentFailure = agent.errorSource === "turn"
  const failure = agentFailure(parentFailure ? undefined : error, agent.id)
  const title = parentFailure
    ? t("agentFailure.parentStopped")
    : failure.kind === "timeout"
      ? t("agentFailure.timeout", { duration: failure.duration })
      : failure.kind === "provider_idle"
        ? t("agentFailure.providerIdle", { duration: failure.duration })
        : failure.kind === "max_turns"
          ? t("agentFailure.maxTurns")
          : t("agentFailure.title")
  const settingsLabel = failure.kind === "timeout"
    ? t("agentFailure.timeoutSettings")
    : failure.kind === "provider_idle"
      ? t("agentFailure.modelSettings")
      : failure.kind === "max_turns"
        ? t("settings.swarm.maxTurns")
        : t("transcript.outputBudgetSettings")
  const hint = failure.kind === "output_budget"
    ? t("transcript.outputBudgetHint")
    : failure.kind === "provider_idle"
      ? t("agentFailure.providerHint")
      : t("agentFailure.limitHint")
  return (
    <div
      role="alert"
      data-testid="agent-failure"
      className="mx-2 mt-2 flex shrink-0 items-start gap-2 rounded border border-destructive/40 bg-destructive/10 px-2 py-2 text-xs text-destructive"
    >
      <AlertTriangle className="mt-0.5 size-4 shrink-0" />
      <div className="min-w-0 flex-1 space-y-1">
        <p className="font-medium">{title}</p>
        <p
          data-testid="agent-failure-error"
          className="max-h-32 overflow-y-auto whitespace-pre-wrap break-words"
        >
          {error ? localizeNotice(error, t.locale) : t("agentFailure.unrecorded")}
        </p>
        {failure.kind !== "unknown" ? (
          <>
            <p>{hint}</p>
            <Button
              type="button"
              variant="link"
              className="h-auto px-0 py-0 text-xs text-destructive underline underline-offset-4"
              onClick={() => openSettings(failure.section, failure.settingsKey)}
            >
              {settingsLabel}
            </Button>
          </>
        ) : null}
      </div>
    </div>
  )
}
