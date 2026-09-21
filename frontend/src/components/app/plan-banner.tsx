import { useEffect, useRef, useState } from "react"
import { ListTodo, Play, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { composerPinClass } from "@/lib/chrome-type"
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

  const preview = markdown?.trim() ? markdown.replace(/\s+/g, " ") : t("plan.empty")

  return (
    <div data-testid="plan-banner" className={composerPinClass}>
      <div className="flex items-center gap-2">
        <ListTodo className="size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <span className="shrink-0 text-muted-foreground">{t("plan.planning")}</span>
        {editing ? (
          <Textarea
            ref={areaRef}
            id="plan-edit"
            data-testid="plan-edit"
            data-edit-draft="true"
            aria-label={t("plan.edit")}
            value={draft}
            rows={6}
            className="min-h-20 flex-1"
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
            className="min-w-0 flex-1 truncate text-left hover:underline"
            aria-label={t("plan.edit")}
            title={markdown?.trim() || undefined}
            onClick={() => {
              if (!onEdit) return
              open.current = true
              setDraft(markdown ?? "")
              setEditing(true)
            }}
          >
            {preview}
          </button>
        )}
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
