import { ChevronRight, PackagePlus, RefreshCw, Trash2 } from "lucide-react"
import { useEffect, useState } from "react"
import Markdown from "react-markdown"

import { ConfirmDeleteDialog } from "@/components/app/confirm-delete-dialog"
import { SkillInstallDialog } from "@/components/app/skill-install-dialog"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { api } from "@/lib/api"
import type { ExternalSkill, ExternalSkillInfo, Project } from "@/lib/types"
import { useT } from "@/lib/use-t"

export function ExternalSkillsDialog({ project, onClose }: { project: Project; onClose: () => void }) {
  const t = useT()
  const projectId = project.id
  const [skills, setSkills] = useState<ExternalSkillInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string>()
  const [installing, setInstalling] = useState(false)
  const [doomed, setDoomed] = useState<string>()
  const [removing, setRemoving] = useState<string>()
  const [removeError, setRemoveError] = useState<{ name: string; error: string }>()
  const [notice, setNotice] = useState<{ action: "skill.installSuccess" | "skill.removeSuccess"; name: string }>()
  const [revision, setRevision] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setLoadError(undefined)
    void api.externalSkills(projectId, controller.signal).then(
      (list) => {
        if (controller.signal.aborted) return
        setSkills(list)
        setLoading(false)
      },
      (e) => {
        if (controller.signal.aborted) return
        setLoadError(e instanceof Error ? e.message : String(e))
        setLoading(false)
      },
    )
    return () => controller.abort()
  }, [projectId, revision])

  const reload = () => setRevision((n) => n + 1)

  const remove = async (name: string) => {
    setRemoving(name)
    setRemoveError(undefined)
    setNotice(undefined)
    try {
      await api.removeExternalSkill(projectId, name)
      setSkills((list) => list.filter((skill) => skill.name !== name))
      setNotice({ action: "skill.removeSuccess", name })
    } catch (e) {
      setRemoveError({ name, error: e instanceof Error ? e.message : String(e) })
    } finally {
      setRemoving(undefined)
    }
  }

  const busy = loading || Boolean(removing)
  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t("skill.external")}</DialogTitle>
          <DialogDescription>{t("skill.externalDesc", { project: project.name })}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <Button
            size="sm"
            onClick={() => {
              setNotice(undefined)
              setRemoveError(undefined)
              setInstalling(true)
            }}
            disabled={busy}
          >
            <PackagePlus />
            {t("skill.install")}
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={reload}
            disabled={busy}
            aria-label={t("memory.reloadNotes")}
            title={t("memory.reloadNotes")}
          >
            <RefreshCw />
          </Button>
        </div>
        {loadError ? (
          <p role="alert" className="break-words text-xs text-destructive">
            {t("skill.installedLoadError", { error: loadError })}
          </p>
        ) : null}
        {removeError ? (
          <p role="alert" className="break-words text-xs text-destructive">
            {t("skill.removeError", removeError)}
          </p>
        ) : null}
        {removing || notice ? (
          <p role="status" className="text-xs text-muted-foreground">
            {removing ? t("skill.removeBusy", { name: removing }) : notice && t(notice.action, notice)}
          </p>
        ) : null}
        {skills.length > 0 ? (
          <div className="thin-scrollbar grid max-h-[55vh] min-w-0 gap-2 overflow-auto">
            {skills.map((skill) => (
              <ExternalSkillRow
                key={`${skill.name}:${skill.updated_at}`}
                projectId={projectId}
                info={skill}
                onRemove={() => setDoomed(skill.name)}
                removeDisabled={busy}
              />
            ))}
          </div>
        ) : !loadError ? (
          <p className="text-xs text-muted-foreground">
            {t(loading ? "memory.loading" : "skill.installedEmpty")}
          </p>
        ) : null}
        {installing ? (
          <SkillInstallDialog
            projectId={projectId}
            onClose={() => setInstalling(false)}
            onInstalled={(skill) => {
              setNotice({ action: "skill.installSuccess", name: skill.name })
              setInstalling(false)
              reload()
            }}
          />
        ) : null}
        <ConfirmDeleteDialog
          open={Boolean(doomed)}
          title={t("skill.removeTitle", { name: doomed ?? "" })}
          description={t("skill.removeDesc")}
          confirmLabel={t("skill.removeConfirm")}
          cancelLabel={t("confirm.cancel")}
          onOpenChange={(next) => { if (!next) setDoomed(undefined) }}
          onConfirm={() => { if (doomed) void remove(doomed) }}
        />
      </DialogContent>
    </Dialog>
  )
}

function ExternalSkillRow({
  projectId,
  info,
  onRemove,
  removeDisabled,
}: {
  projectId: string
  info: ExternalSkillInfo
  onRemove: () => void
  removeDisabled: boolean
}) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [skill, setSkill] = useState<ExternalSkill>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  const toggle = async () => {
    const next = !open
    setOpen(next)
    if (!next || skill || loading) return
    setError(undefined)
    setLoading(true)
    try {
      setSkill(await api.externalSkill(projectId, info.name))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div data-testid="installed-skill-card" className="min-w-0 overflow-hidden rounded-md border border-border bg-card">
      <div className="flex items-start gap-1 px-2 py-1.5">
        <button
          type="button"
          onClick={() => void toggle()}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-start gap-2 text-left"
        >
          <ChevronRight className={`mt-0.5 size-3.5 shrink-0 transition-transform ${open ? "rotate-90" : ""}`} />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm">{info.name}</span>
            <span className="block text-xs leading-snug text-muted-foreground">{info.description}</span>
          </span>
        </button>
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={onRemove}
          disabled={removeDisabled}
          aria-label={t("skill.remove", { name: info.name })}
          title={t("skill.remove", { name: info.name })}
        >
          <Trash2 />
        </Button>
      </div>
      {open ? (
        <div
          data-testid="skill-body"
          className="md thin-scrollbar max-h-72 min-w-0 overflow-auto wrap-break-word border-t border-border px-2 py-2 text-sm *:first:mt-0"
        >
          {error ? <p role="alert" className="text-xs text-destructive">{error}</p> : skill ? (
            // A bundle preview must not fetch its external images or navigate to relative paths on the engine.
            <Markdown skipHtml disallowedElements={["img", "a"]} unwrapDisallowed>
              {skill.body}
            </Markdown>
          ) : <p className="text-xs text-muted-foreground">{t("memory.loading")}</p>}
        </div>
      ) : null}
    </div>
  )
}
