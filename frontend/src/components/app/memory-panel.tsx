import { ChevronRight, RefreshCw, Sparkles, Trash2 } from "lucide-react"
import { useEffect, useState } from "react"

import { MemoMarkdown } from "@/components/app/markdown"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import type { Project, ProjectMemory, Skill } from "@/lib/types"

export interface MemoryPanelProps {
  project?: Project
  memory?: ProjectMemory
  loading: boolean
  onSave: (text: string) => Promise<void>
  onDeleteSkill: (name: string) => void
  onRefresh: () => void
  onReview: () => void
  /** Only wired where the host can open a file manager. */
  onReveal?: () => void
}

/** What a project has learned: the notes every turn carries, and the
 *  procedures it recorded for itself.
 *
 *  Both are editable here on purpose. Memory a user cannot read and correct is
 *  memory nobody trusts, and a wrong note would otherwise be repeated in every
 *  future conversation with no way to stop it. */
export function MemoryPanel({
  project,
  memory,
  loading,
  onSave,
  onDeleteSkill,
  onRefresh,
  onReview,
  onReveal,
}: MemoryPanelProps) {
  const [draft, setDraft] = useState("")
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const text = memory?.memory.text ?? ""

  useEffect(() => {
    // A review lands while the panel is open, so the stored text has to be
    // adopted — but not over something the user is in the middle of typing,
    // which is the worse of the two failures.
    if (dirty) return
    setDraft(text)
  }, [text, dirty])

  if (!project) {
    return (
      <p className="p-4 text-sm text-muted-foreground">
        This conversation is not in a project, so it has nothing to remember
        between conversations.
      </p>
    )
  }

  const save = async () => {
    setSaving(true)
    setError(undefined)
    try {
      await onSave(draft)
      setDirty(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  const chars = memory?.memory.chars ?? 0
  const limit = memory?.memory.limit ?? 0

  return (
    <div className="flex flex-col gap-4 p-3">
      <div className="flex items-center gap-2">
        <p className="min-w-0 flex-1 truncate text-sm font-medium">
          {project.name}
        </p>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onReview}
          aria-label="Review this conversation now"
          title="Review this conversation now"
        >
          <Sparkles />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onRefresh}
          aria-label="Reload memory"
          title="Reload memory"
        >
          <RefreshCw />
        </Button>
      </div>

      {memory && !memory.enabled ? (
        <p className="rounded-md border border-border p-2 text-xs text-muted-foreground">
          Memory is switched off for this project. What is already stored stays
          here and is not used.
        </p>
      ) : null}

      <section className="grid gap-2">
        <div className="flex items-center gap-2">
          <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Notes
          </h3>
          <Badge variant={chars > limit * 0.9 ? "warning" : "outline"}>
            {chars}/{limit}
          </Badge>
        </div>
        <p className="text-xs text-muted-foreground">
          Carried into every conversation in this project. One note per
          paragraph.
        </p>
        <Textarea
          aria-label="Project notes"
          rows={8}
          value={draft}
          disabled={loading}
          onChange={(e) => {
            setDraft(e.target.value)
            setDirty(true)
          }}
        />
        {error ? <p className="text-xs text-destructive">{error}</p> : null}
        <div className="flex items-center gap-2">
          <Button size="sm" disabled={!dirty || saving} onClick={() => void save()}>
            Save notes
          </Button>
          {dirty ? (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                setDraft(text)
                setDirty(false)
                setError(undefined)
              }}
            >
              Revert
            </Button>
          ) : null}
        </div>
      </section>

      <section className="grid gap-2">
        <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          Skills
        </h3>
        {memory && memory.skills.length > 0 ? (
          memory.skills.map((skill) => (
            <SkillRow
              key={skill.name}
              projectId={project.id}
              name={skill.name}
              description={skill.description}
              onDelete={() => onDeleteSkill(skill.name)}
            />
          ))
        ) : (
          <p className="text-xs text-muted-foreground">
            No procedures recorded yet. They appear here when a conversation
            produces one worth following again.
          </p>
        )}
      </section>

      <p className="break-all text-xs text-muted-foreground">
        {memory?.dir ?? project.memory_dir}
        {onReveal ? (
          <Button variant="link" size="sm" onClick={onReveal}>
            Show in Finder
          </Button>
        ) : null}
      </p>
    </div>
  )
}

/** One skill, expanded on demand. The body is fetched when it is opened
 *  rather than with the list, for the same reason the prompt does not inline
 *  it: most of them are not what anyone came to read. */
function SkillRow({
  projectId,
  name,
  description,
  onDelete,
}: {
  projectId: string
  name: string
  description: string
  onDelete: () => void
}) {
  const [open, setOpen] = useState(false)
  const [skill, setSkill] = useState<Skill>()
  const [error, setError] = useState<string>()

  const toggle = async () => {
    const next = !open
    setOpen(next)
    if (!next || skill) return
    try {
      setSkill(await api.skill(projectId, name))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <div className="rounded-md border border-border">
      <div className="flex items-center gap-1 px-2 py-1.5">
        <button
          type="button"
          onClick={() => void toggle()}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-2 text-left"
        >
          <ChevronRight
            className={`size-3.5 shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
          />
          <span className="min-w-0">
            <span className="block truncate text-sm">{name}</span>
            <span className="block truncate text-xs text-muted-foreground">
              {description}
            </span>
          </span>
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onDelete}
          aria-label={`Delete the skill ${name}`}
          title="Delete"
        >
          <Trash2 />
        </Button>
      </div>
      {open ? (
        <div className="md thin-scrollbar max-h-72 overflow-auto border-t border-border px-2 py-2 text-sm [&>:first-child]:mt-0">
          {error ? (
            <p className="text-xs text-destructive">{error}</p>
          ) : skill ? (
            <MemoMarkdown text={skill.body} />
          ) : (
            <p className="text-xs text-muted-foreground">Loading…</p>
          )}
        </div>
      ) : null}
    </div>
  )
}
