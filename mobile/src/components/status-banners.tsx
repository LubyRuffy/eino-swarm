import { useEffect, useState } from "react"
import { Clock, Flag, Play } from "lucide-react"

import { Button } from "@/components/ui/button"
import { formatGoalAge, goalState, parseGoalReason } from "@/lib/goal"
import { t } from "@/lib/i18n"
import type { ThreadDetail } from "@/lib/rpc"
import { scheduleHeadline } from "@/lib/schedule"
import { cn } from "@/lib/cn"

const FAILED_TURN_BLOCK = "the last turn failed"

export function GoalBanner({
  detail,
  running,
  disabled,
  waiting,
  onResume,
}: {
  detail: ThreadDetail
  running?: boolean
  disabled?: boolean
  waiting?: boolean
  onResume?: () => void
}) {
  const state = goalState(detail)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!detail.goal_started_at || state === "done") return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [detail.goal_started_at, state])
  if (!state || state === "done" || !(detail.goal ?? "").trim()) return null
  const started = detail.goal_started_at ? new Date(detail.goal_started_at) : null
  const age =
    started && !Number.isNaN(started.getTime())
      ? formatGoalAge(started, new Date(now))
      : ""
  const held = state === "paused"
  const paused = held || state === "blocked"
  const canStart = !running && Boolean(onResume) && (held || state === "blocked")
  const label =
    paused ? t("goal.executionPaused") : t("goal.pursuing")
  const reason =
    state === "blocked"
      ? displayBlock(detail.goal_block_reason)
      : held && !running
        ? detail.goal_idle && !detail.goal_capped
          ? t("goal.idleReason")
          : t("goal.capReason")
        : waiting && !running
          ? t("goal.waitHint")
          : ""

  return (
    <div data-testid="goal-banner" className="min-w-0 shrink-0 overflow-hidden border-b border-border bg-card px-3 py-2">
      <div className="flex items-start gap-2">
        <Flag className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span
              className={cn(
                "rounded-md border px-1.5 py-0.5 text-[11px] font-medium",
                state === "blocked"
                  ? "border-destructive/40 text-destructive"
                  : held
                    ? "border-[hsl(var(--running))] text-[hsl(var(--running))]"
                    : "border-border text-muted-foreground",
              )}
            >
              {label}
            </span>
            {age ? (
              <span data-testid="goal-age" className="text-xs tabular-nums text-muted-foreground">
                {t("goal.age", { age })}
              </span>
            ) : null}
          </div>
          <p data-testid="goal-text" className="mt-1 truncate text-sm">
            {detail.goal}
          </p>
        </div>
        {canStart ? (
          <Button
            type="button"
            variant="outline"
            className="h-8 shrink-0 px-2 text-xs"
            aria-label={t("goal.start")}
            disabled={disabled}
            onClick={onResume}
          >
            <Play className="size-3.5" />
            {t("goal.start")}
          </Button>
        ) : null}
      </div>
      {reason ? (
        <p data-testid="goal-reason" className="mt-2 break-words text-xs text-muted-foreground">
          {paused ? t("goal.pauseReason", { reason }) : reason}
        </p>
      ) : null}
      {canStart ? (
        <p className="mt-1 break-words text-xs text-muted-foreground">
          {t("goal.manualWhilePaused")}
        </p>
      ) : null}
    </div>
  )
}

function displayBlock(reason?: string): string {
  const why = (reason ?? "").trim()
  if (why === FAILED_TURN_BLOCK) return t("goal.failedTurn")
  return parseGoalReason(why) || why
}

export function ScheduleBanner({
  detail,
  running,
  disabled,
  onRunNow,
  onCancel,
}: {
  detail: ThreadDetail
  running?: boolean
  disabled?: boolean
  onRunNow?: () => void
  onCancel?: () => void
}) {
  if (!detail.waiting || running || !detail.wake) return null
  const when = detail.wake.next_run_at ? formatWhen(detail.wake.next_run_at) : ""
  const headline = scheduleHeadline(detail.wake)
  return (
    <div data-testid="schedule-banner" className="min-w-0 shrink-0 overflow-hidden border-b border-border bg-card px-3 py-2">
      <div className="flex items-start gap-2">
        <Clock className="mt-1 size-3.5 shrink-0 text-[hsl(var(--running))]" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="rounded-md border border-[hsl(var(--running))] px-1.5 py-0.5 text-[11px] font-medium text-[hsl(var(--running))]">
              {t("schedule.waiting")}
            </span>
            {when ? (
              <span className="text-xs text-muted-foreground">
                {t("schedule.nextCheck", { time: when })}
              </span>
            ) : null}
          </div>
          {headline ? <p className="mt-1 truncate text-sm">{headline}</p> : null}
        </div>
        <div className="flex shrink-0 flex-col gap-1">
          {onRunNow ? (
            <Button
              type="button"
              variant="outline"
              className="h-8 px-2 text-xs"
              aria-label={t("schedule.runNow")}
              disabled={disabled}
              onClick={onRunNow}
            >
              <Play className="size-3.5" />
              {t("schedule.runNow")}
            </Button>
          ) : null}
          {onCancel ? (
            <Button
              type="button"
              variant="ghost"
              className="h-8 px-2 text-xs"
              aria-label={t("schedule.cancel")}
              disabled={disabled}
              onClick={onCancel}
            >
              {t("schedule.cancel")}
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function formatWhen(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString()
}
