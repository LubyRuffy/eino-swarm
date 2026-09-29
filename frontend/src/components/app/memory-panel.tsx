import { ChevronRight, Copy, Layers, Loader2, RefreshCw, Sparkles, Trash2 } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { MemoMarkdown } from "@/components/app/markdown"
import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { SkillCopyDialog, SkillPullDialog } from "@/components/app/skill-copy-dialog"
import { SkillTidyCard } from "@/components/app/skill-tidy-card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import type { Project, ProjectMemory, Skill, SkillOrigin, SkillTidyReport, TidyLive } from "@/lib/types"
import { useT } from "@/lib/use-t"

export interface MemoryPanelProps {
  project?: Project
  memory?: ProjectMemory
  loading: boolean
  onSave: (text: string) => Promise<void>
  onDeleteSkill: (name: string) => void
  onRefresh: () => void
  onReview: () => void
  onTidySkills: () => void
  /** A click is in flight; the matching memory_review has not arrived. */
  reviewing?: boolean
  /** What the last click decided, once it has an answer. */
  reviewHint?: string
  /** True between a tidy click and the response. */
  tidying?: boolean
  /** What the last tidy decided. Absent until a click has an answer. */
  tidyReport?: SkillTidyReport
  tidyError?: string
  /** Model prose and skill writes while the tidy request is still open. */
  tidyLive?: TidyLive
  onDismissTidy?: () => void
  /** Only wired where the host can open a file manager. */
  onReveal?: () => void
  /** A write landed while this tab was not the one on screen. */
  unread?: boolean
  /** Called while the panel is on screen, so a write that lands here is not
   *  also a badge on the tab the user is already looking at. */
  onSeen?: () => void
  /** Skill name the sidebar asked to open. The body is fetched the same way
   *  a click would: it is not in the list payload. */
  focusSkill?: string
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
  onTidySkills,
  reviewing,
  reviewHint,
  tidying,
  tidyReport,
  tidyError,
  tidyLive,
  onDismissTidy,
  onReveal,
  onSeen,
  focusSkill,
}: MemoryPanelProps) {
  const t = useT()
  const [draft, setDraft] = useState("")
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [conflict, setConflict] = useState(false)
  const [doomedSkill, setDoomedSkill] = useState<string>()
  const [copyTarget, setCopyTarget] = useState<{ names?: string[] }>()
  const [copyNotice, setCopyNotice] = useState<string>()

  useEffect(() => {
    setCopyNotice(undefined)
  }, [project?.id])
  const text = memory?.memory.text ?? ""
  const seen = useRef(text)

  useEffect(() => {
    onSeen?.()
  }, [onSeen, memory?.memory.rev])

  useEffect(() => {
    // A review lands while the panel is open, so the stored text has to be
    // adopted — but not over something the user is in the middle of typing,
    // which is the worse of the two failures. That case is a conflict: show
    // both, and let them pick.
    if (dirty) {
      if (seen.current !== text) setConflict(true)
      seen.current = text
      return
    }
    setDraft(text)
    setConflict(false)
    seen.current = text
  }, [text, dirty])

  if (!project) {
    return (
      <p className="p-4 text-sm text-muted-foreground">
        {t("memory.noProject")}
      </p>
    )
  }

  const save = async () => {
    setSaving(true)
    setError(undefined)
    try {
      await onSave(draft)
      setDirty(false)
      setConflict(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      if (isConflict(e)) setConflict(true)
    } finally {
      setSaving(false)
    }
  }

  const reload = () => {
    setDraft(text)
    setDirty(false)
    setConflict(false)
    setError(undefined)
  }

  const chars = memory?.memory.chars ?? 0
  const limit = memory?.memory.limit ?? 0

  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col gap-3 p-3">
      <div className="flex shrink-0 items-center gap-2">
        <p className="min-w-0 flex-1 truncate text-sm font-medium">
          {project.name}
        </p>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onReview}
          disabled={reviewing}
          aria-busy={reviewing}
          aria-label={t("memory.reviewNow")}
          title={t("memory.reviewNow")}
        >
          {reviewing ? <Loader2 className="animate-spin" /> : <Sparkles />}
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onRefresh}
          aria-label={t("memory.reload")}
          title={t("memory.reload")}
        >
          <RefreshCw />
        </Button>
      </div>

      {reviewing || reviewHint ? (
        <p role="status" data-testid="review-status" className="text-xs text-muted-foreground">
          {reviewing ? t("memory.reviewing") : reviewHint}
        </p>
      ) : null}

      {memory && !memory.enabled ? (
        <p className="rounded-md border border-border p-2 text-xs text-muted-foreground">
          {t("memory.off")}
        </p>
      ) : null}

      <section className="grid shrink-0 gap-2">
        <div className="flex items-center gap-2">
          <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {t("memory.notes")}
          </h3>
          <Badge variant={chars > limit * 0.9 ? "warning" : "outline"}>
            {chars}/{limit}
          </Badge>
        </div>
        <p className="text-xs text-muted-foreground">
          {t("memory.notesHint")}
        </p>
        <Textarea
          aria-label={t("memory.notesLabel")}
          // A tall notes box used to push Skills off the window, and the pane
          // could not scroll. Cap the box; leftover notes scroll inside it.
          rows={5}
          className="max-h-36 overflow-y-auto"
          value={draft}
          disabled={loading}
          onChange={(e) => {
            const next = e.target.value
            setDraft(next)
            // Compare to what is stored, not "did a key fire": typing back
            // to the saved text is not an edit, so Save stays gone.
            setDirty(next !== text)
          }}
        />
        {conflict ? (
          <div
            role="status"
            className="rounded-md border border-border bg-muted/60 px-2 py-2 text-xs"
          >
            <p>
              {t("memory.conflict")}
            </p>
            {text && text !== draft ? (
              <p className="mt-1 line-clamp-3 text-muted-foreground">
                {t("memory.storedNow", { text })}
              </p>
            ) : null}
          </div>
        ) : null}
        {error ? <p className="text-xs text-destructive">{error}</p> : null}
        {dirty || conflict ? (
          <div className="flex items-center gap-2">
            {dirty ? (
              <Button size="sm" disabled={saving} onClick={() => void save()}>
                {t("memory.saveNotes")}
              </Button>
            ) : null}
            <Button size="sm" variant="ghost" onClick={reload}>
              {conflict ? t("memory.reloadNotes") : t("memory.revert")}
            </Button>
          </div>
        ) : null}
      </section>

      <section className="flex min-h-0 flex-1 flex-col gap-2">
        <div className="flex shrink-0 items-center gap-2">
          <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            {t("memory.skills")}
          </h3>
          <Button
            variant={memory?.needs_tidy ? "outline" : "ghost"}
            size="icon-sm"
            onClick={onTidySkills}
            disabled={tidying}
            aria-busy={tidying}
            aria-label={t("memory.tidySkills")}
            title={t("memory.tidySkills")}
          >
            {tidying ? <Loader2 className="animate-spin" /> : <Layers />}
          </Button>
          {memory && memory.skills.length > 1 ? (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setCopyTarget({})}
              aria-label={t("skill.copyAll")}
              title={t("skill.copyAll")}
            >
              <Copy />
            </Button>
          ) : null}
        </div>
        <p className="text-xs text-muted-foreground">{t("memory.skillsHint")}</p>
        {copyNotice ? (
          <p data-testid="skill-copy-notice" className="text-xs text-muted-foreground">
            {copyNotice}
          </p>
        ) : null}
        <SkillTidyCard
          tidying={Boolean(tidying)}
          skillCount={memory?.skills.length ?? 0}
          report={tidyReport}
          error={tidyError}
          live={tidyLive}
          onDismiss={onDismissTidy}
        />
        <div
          data-testid="skills-list"
          className="thin-scrollbar min-h-0 min-w-0 flex-1 overflow-auto"
        >
          {memory && memory.skills.length > 0 ? (
            <div className="grid min-w-0 gap-2">
              {memory.skills.map((skill) => (
                <SkillRow
                  key={skill.name}
                  projectId={project.id}
                  name={skill.name}
                  description={skill.description}
                  origin={skill.origin}
                  startOpen={skill.name === focusSkill}
                  onDelete={() => setDoomedSkill(skill.name)}
                  onCopy={() => setCopyTarget({ names: [skill.name] })}
                  onRefresh={onRefresh}
                />
              ))}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              {t("memory.noSkills")}
            </p>
          )}
        </div>
      </section>

      <p className="shrink-0 break-all text-xs text-muted-foreground">
        {memory?.dir ?? project.memory_dir}
        {onReveal ? (
          <Button variant="link" size="sm" onClick={onReveal}>
            {t("memory.showInFinder")}
          </Button>
        ) : null}
      </p>
      <ConfirmDeleteDialog
        open={Boolean(doomedSkill)}
        title={t("skill.deleteTitle", { name: doomedSkill ?? "" })}
        description={t("skill.deleteDesc")}
        confirmLabel={t("skill.deleteConfirm")}
        cancelLabel={t("confirm.cancel")}
        onOpenChange={(open) => {
          if (!open) setDoomedSkill(undefined)
        }}
        onConfirm={() => {
          if (doomedSkill) onDeleteSkill(doomedSkill)
        }}
      />
      <SkillCopyDialog
        open={copyTarget !== undefined}
        sourceId={project.id}
        names={copyTarget?.names}
        onOpenChange={(open) => {
          if (!open) setCopyTarget(undefined)
        }}
        onCopied={setCopyNotice}
      />
    </div>
  )
}

function isConflict(e: unknown): boolean {
  return Boolean(e && typeof e === "object" && "code" in e && (e as { code?: string }).code === "conflict")
}

/** One skill, expanded on demand. The body is fetched when it is opened
 *  rather than with the list, for the same reason the prompt does not inline
 *  it: most of them are not what anyone came to read. */
function originLabel(
  origin: SkillOrigin,
  t: (key: "skill.originFrom" | "skill.originLocal" | "skill.originDiverged" | "skill.originMissing", vars?: { project: string }) => string,
) {
  const project = origin.project_name || origin.project_id
  switch (origin.status) {
    case "local":
      return t("skill.originLocal")
    case "diverged":
      return t("skill.originDiverged")
    case "missing":
      return t("skill.originMissing")
    default:
      return t("skill.originFrom", { project })
  }
}

function SkillRow({
  projectId,
  name,
  description,
  origin,
  startOpen,
  onDelete,
  onCopy,
  onRefresh,
}: {
  projectId: string
  name: string
  description: string
  origin?: SkillOrigin
  startOpen?: boolean
  onDelete: () => void
  onCopy: () => void
  onRefresh: () => void
}) {
  const t = useT()
  const [open, setOpen] = useState(Boolean(startOpen))
  const [skill, setSkill] = useState<Skill>()
  const [error, setError] = useState<string>()
  const [replacing, setReplacing] = useState(false)
  const canPull = origin?.status === "update" || origin?.status === "diverged"

  useEffect(() => {
    // The sidebar sent the user here. Opening without fetching would show
    // "Loading…" forever, and fetching without opening would hide the body
    // they came to read.
    if (!startOpen) return
    let cancelled = false
    setOpen(true)
    void api.skill(projectId, name).then(
      (got) => {
        if (!cancelled) setSkill(got)
      },
      (e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : String(e))
      },
    )
    return () => {
      cancelled = true
    }
  }, [startOpen, projectId, name])

  const pull = async (force: boolean) => {
    try {
      const got = await api.pullSkill(projectId, name, force)
      setSkill(got)
      setError(undefined)
      onRefresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

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
    <div
      data-testid="skill-card"
      className="relative w-full min-w-0 overflow-hidden rounded-md border border-border bg-card"
    >
      <div className="flex items-start gap-1 px-2 py-1.5">
        <button
          type="button"
          onClick={() => void toggle()}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-start gap-2 text-left"
        >
          <ChevronRight
            className={`mt-0.5 size-3.5 shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
          />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm">{name}</span>
            <span className="block text-xs leading-snug text-muted-foreground">
              {description}
            </span>
          </span>
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onCopy}
          aria-label={t("skill.copyNamed", { name })}
          title={t("skill.copyNamed", { name })}
        >
          <Copy />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onDelete}
          aria-label={t("memory.deleteSkill", { name })}
          title={t("memory.delete")}
        >
          <Trash2 />
        </Button>
      </div>
      {origin ? (
        <div className="flex items-center gap-2 px-2 pb-1.5">
          <p data-testid="skill-origin" className="min-w-0 flex-1 text-xs text-muted-foreground">
            {originLabel(origin, t)}
          </p>
          {canPull ? (
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                if (origin.status === "diverged") {
                  setReplacing(true)
                  return
                }
                void pull(false)
              }}
              aria-label={t("skill.updateFrom", { name })}
            >
              {t("skill.update")}
            </Button>
          ) : null}
        </div>
      ) : null}
      <SkillPullDialog
        open={replacing}
        onOpenChange={setReplacing}
        onConfirm={() => void pull(true)}
      />
      {open ? (
        <div
          data-testid="skill-body"
          className="md thin-scrollbar max-h-72 min-w-0 overflow-auto break-words border-t border-border px-2 py-2 text-sm [&>:first-child]:mt-0"
        >
          {error ? (
            <p className="text-xs text-destructive">{error}</p>
          ) : skill ? (
            <MemoMarkdown text={skill.body} />
          ) : (
            <p className="text-xs text-muted-foreground">{t("memory.loading")}</p>
          )}
        </div>
      ) : null}
    </div>
  )
}
