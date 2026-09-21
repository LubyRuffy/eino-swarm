import { useEffect, useRef, useState } from "react"
import { Flag, Play, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { composerPinClass } from "@/lib/chrome-type"
import { useT, type Translate } from "@/lib/use-t"

/** Stored when a pursuing turn died before block_goal. Keep in sync with
 *  engine.goalBlockedByFailedTurn — older conversations still have this. */
export const FAILED_TURN_BLOCK_REASON = "the last turn failed"

/** Banner copy for a block. The sentinel is a protocol string; show the
 *  turn's public error when we have it, else a localized line. */
export function displayGoalBlockReason(
  reason: string,
  turnError: string | undefined,
  t: Translate,
): string {
  const why = reason.trim()
  const err = turnError?.trim() ?? ""
  if (why === FAILED_TURN_BLOCK_REASON) return err || t("goal.failedTurn")
  return why
}

/** Elapsed since the objective was set, Codex-style `1d 10h 39m 16s`. */
export function formatGoalAge(from: Date, now = new Date()): string {
  let s = Math.max(0, Math.floor((now.getTime() - from.getTime()) / 1000))
  const d = Math.floor(s / 86400)
  s %= 86400
  const h = Math.floor(s / 3600)
  s %= 3600
  const m = Math.floor(s / 60)
  s %= 60
  const parts: string[] = []
  if (d) parts.push(`${d}d`)
  if (h || d) parts.push(`${h}h`)
  if (m || h || d) parts.push(`${m}m`)
  parts.push(`${s}s`)
  return parts.join(" ")
}

/** The standing objective for this conversation, when one is set. */
export function GoalBanner({
  goal,
  complete,
  blocked,
  blockReason,
  turnError,
  capped,
  idle,
  running,
  startedAt,
  waiting,
  onClear,
  onEdit,
  onResume,
}: {
  goal: string
  complete?: boolean
  blocked?: boolean
  blockReason?: string
  /** Public error of the failed turn. Used when blockReason is the sentinel. */
  turnError?: string
  capped?: boolean
  /** True after a no-progress continuation. Play resumes, like a cap. */
  idle?: boolean
  running?: boolean
  startedAt?: string
  /** Parked on a thread wake. Auto-continue is waiting, not stuck. */
  waiting?: boolean
  onClear: () => void
  onEdit?: (text: string) => void
  onResume?: () => void
}) {
  const t = useT()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(goal)
  const [now, setNow] = useState(() => Date.now())
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const open = useRef(false)

  useEffect(() => {
    if (!editing) setDraft(goal)
  }, [goal, editing])

  useEffect(() => {
    if (editing) areaRef.current?.focus()
  }, [editing])

  useEffect(() => {
    if (!startedAt || complete) return
    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [startedAt, complete])

  if (!goal.trim()) return null

  const held = Boolean(capped || idle)
  const state = complete
    ? t("goal.done")
    : blocked
      ? t("goal.blocked")
      : held
        ? t("goal.paused")
        : t("goal.pursuing")
  // Codex: Play is resume, not "the last turn ended". An active goal
  // between auto-continue sessions stays Pursuing with no control.
  // Done used to hide Play, so a mistaken complete_goal had no one-click
  // undo. Start reopens that case the same way a cap does.
  const canStart =
    !running && Boolean(onResume) && Boolean(capped || blocked || idle || complete)
  const started = startedAt ? new Date(startedAt) : null
  const age =
    started && !Number.isNaN(started.getTime())
      ? formatGoalAge(started, new Date(now))
      : ""

  const reason = blocked && (blockReason?.trim() || turnError?.trim())
    ? displayGoalBlockReason(blockReason ?? "", turnError, t)
    : complete && !running
      ? t("goal.completeHint")
      : held && !running
        ? idle && !capped
          ? t("goal.idleHint")
          : t("goal.capHint")
        : ""

  const save = () => {
    if (!open.current) return
    open.current = false
    const next = draft.trim()
    setEditing(false)
    if (!next) {
      onClear()
      return
    }
    if (next !== goal.trim()) onEdit?.(next)
  }

  return (
    <div
      data-testid="goal-banner"
      data-waiting={waiting ? "true" : undefined}
      className={composerPinClass}
    >
      <div className="flex items-center gap-2">
        <Flag className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <span className="shrink-0 text-muted-foreground">{state}</span>
        {editing ? (
          <Textarea
            ref={areaRef}
            id="goal-edit"
            data-testid="goal-edit"
            data-edit-draft="true"
            aria-label={t("goal.edit")}
            value={draft}
            rows={2}
            className="min-h-12 flex-1"
            onChange={(e) => setDraft(e.target.value)}
            onBlur={save}
            onKeyDown={(e) => {
              if (e.key === "Escape") {
                e.preventDefault()
                open.current = false
                setDraft(goal)
                setEditing(false)
              } else if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault()
                save()
              }
            }}
          />
        ) : (
          <button
            type="button"
            data-testid="goal-text"
            className="min-w-0 flex-1 truncate text-left hover:underline"
            aria-label={t("goal.edit")}
            title={reason || goal}
            onClick={() => {
              if (!onEdit) return
              open.current = true
              setDraft(goal)
              setEditing(true)
            }}
          >
            {goal}
          </button>
        )}
        {age ? (
          <span
            data-testid="goal-age"
            className="shrink-0 tabular-nums text-muted-foreground"
          >
            {age}
          </span>
        ) : null}
        {canStart ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            className="shrink-0"
            data-testid="goal-start"
            aria-label={t("goal.start")}
            onClick={onResume}
          >
            <Play />
          </Button>
        ) : null}
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t("goal.clear")}
          onClick={onClear}
        >
          <X />
        </Button>
      </div>
      {reason ? (
        <p
          data-testid="goal-reason"
          className="mt-0.5 truncate text-muted-foreground"
          title={reason}
        >
          {reason}
        </p>
      ) : null}
    </div>
  )
}
