import { useEffect, useRef, useState } from "react"
import { Flag, Play, X } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
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
  const badge = complete ? "success" : blocked ? "danger" : held ? "warning" : "outline"
  // Codex: Play is resume, not "the last turn ended". An active goal
  // between auto-continue sessions stays Pursuing with no control.
  const canStart =
    !complete && !running && Boolean(onResume) && Boolean(capped || blocked || idle)
  const started = startedAt ? new Date(startedAt) : null
  const age =
    started && !Number.isNaN(started.getTime())
      ? formatGoalAge(started, new Date(now))
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
    <div data-testid="goal-banner" className="mb-2 rounded-xl border bg-card px-3 py-2">
      <div className="flex items-start gap-2">
        <Flag className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant={badge}>{state}</Badge>
            {age ? (
              <span data-testid="goal-age" className="text-xs tabular-nums text-muted-foreground">
                {age}
              </span>
            ) : null}
          </div>
          {editing ? (
            <Textarea
              ref={areaRef}
              id="goal-edit"
              data-testid="goal-edit"
              data-edit-draft="true"
              aria-label={t("goal.edit")}
              value={draft}
              rows={3}
              className="mt-2 min-h-16"
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
              className="mt-1 block w-full truncate text-left text-sm hover:underline"
              aria-label={t("goal.edit")}
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
          {blocked && (blockReason?.trim() || turnError?.trim()) ? (
            <p
              data-testid="goal-reason"
              className="mt-1 whitespace-pre-wrap break-words text-xs text-muted-foreground"
            >
              {displayGoalBlockReason(blockReason ?? "", turnError, t)}
            </p>
          ) : null}
        </div>
        {canStart ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
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
    </div>
  )
}
