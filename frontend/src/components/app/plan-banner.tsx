import { useEffect, useRef, useState } from "react"
import { ListTodo, Play, X } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { useT } from "@/lib/use-t"

/** Planning banner: draft markdown, Implement, leave planning. */
export function PlanBanner({
  mode,
  markdown,
  running,
  onImplement,
  onEdit,
  onLeave,
}: {
  mode?: boolean
  markdown?: string
  running?: boolean
  onImplement?: () => void
  onEdit?: (text: string) => void
  onLeave?: () => void
}) {
  const t = useT()
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(markdown ?? "")
  const areaRef = useRef<HTMLTextAreaElement>(null)
  const open = useRef(false)

  useEffect(() => {
    if (!editing) setDraft(markdown ?? "")
  }, [markdown, editing])

  useEffect(() => {
    if (editing) areaRef.current?.focus()
  }, [editing])

  if (!mode) return null

  const save = () => {
    if (!open.current) return
    open.current = false
    const next = draft.trim()
    setEditing(false)
    if (next && next !== (markdown ?? "").trim()) onEdit?.(next)
  }

  return (
    <div data-testid="plan-banner" className="mb-2 rounded-xl border bg-card px-3 py-2">
      <div className="flex items-start gap-2">
        <ListTodo className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0 flex-1">
          <Badge variant="outline">{t("plan.planning")}</Badge>
          {editing ? (
            <Textarea
              ref={areaRef}
              id="plan-edit"
              data-testid="plan-edit"
              data-edit-draft="true"
              aria-label={t("plan.edit")}
              value={draft}
              rows={8}
              className="mt-2 min-h-24"
              onChange={(e) => setDraft(e.target.value)}
              onBlur={save}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  e.preventDefault()
                  open.current = false
                  setDraft(markdown ?? "")
                  setEditing(false)
                }
              }}
            />
          ) : (
            <button
              type="button"
              data-testid="plan-text"
              className="mt-1 block max-h-32 w-full overflow-hidden text-left text-sm whitespace-pre-wrap hover:underline"
              aria-label={t("plan.edit")}
              onClick={() => {
                if (!onEdit) return
                open.current = true
                setDraft(markdown ?? "")
                setEditing(true)
              }}
            >
              {markdown?.trim() ? markdown : t("plan.empty")}
            </button>
          )}
        </div>
        {markdown?.trim() && !running ? (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            data-testid="plan-implement"
            aria-label={t("plan.implement")}
            onClick={onImplement}
          >
            <Play />
          </Button>
        ) : null}
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t("plan.leave")}
          onClick={onLeave}
        >
          <X />
        </Button>
      </div>
    </div>
  )
}
