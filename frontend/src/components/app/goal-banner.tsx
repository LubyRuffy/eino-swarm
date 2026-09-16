import { useEffect, useRef, useState } from "react"
import { Flag, Play, X } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { useT } from "@/lib/use-t"

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
  capped,
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
  capped?: boolean
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

  const state = complete
    ? t("goal.done")
    : blocked
      ? t("goal.blocked")
      : capped
        ? t("goal.paused")
        : t("goal.pursuing")
  const badge = complete ? "success" : blocked ? "danger" : capped ? "warning" : "outline"
  const canStart = !complete && !running && Boolean(onResume)
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
              aria-label={t("goal.edit")}
              value={draft}
              rows={3}
              className="mt-2 min-h-16 border border-input"
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
          {blocked && blockReason?.trim() ? (
            <p data-testid="goal-reason" className="mt-1 text-xs text-muted-foreground">
              {blockReason.trim()}
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
