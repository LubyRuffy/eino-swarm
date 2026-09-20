import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { ApiError, type ProjectPatch } from "@/lib/api"
import type { Project } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

/** Create or edit one project: its instruction, its working directory and
 *  whether it remembers anything. */
export function ProjectDialog({
  open,
  project,
  memoryAvailable,
  onOpenChange,
  onSave,
}: {
  open: boolean
  /** Undefined creates a project; a project edits it. */
  project?: Project
  /** False when memory is off for the whole install, so the per-project
   *  switch does not promise something the app will not do. */
  memoryAvailable: boolean
  onOpenChange: (open: boolean) => void
  onSave: (patch: ProjectPatch) => Promise<Project>
}) {
  const t = useT()
  const [name, setName] = useState("")
  const [prompt, setPrompt] = useState("")
  const [workdir, setWorkdir] = useState("")
  const [memory, setMemory] = useState(true)
  const [saving, setSaving] = useState(false)
  const [workdirError, setWorkdirError] = useState<string>()

  useEffect(() => {
    if (!open) return
    setName(project?.name ?? "")
    setPrompt(project?.system_prompt ?? "")
    setWorkdir(project?.workdir ?? "")
    setMemory(project ? project.memory_enabled : memoryAvailable)
    setWorkdirError(undefined)
    useToasts.getState().dismiss("project:save")
  }, [open, project, memoryAvailable])

  const submit = async () => {
    setSaving(true)
    setWorkdirError(undefined)
    useToasts.getState().dismiss("project:save")
    try {
      await onSave({
        name: name.trim(),
        system_prompt: prompt,
        workdir: workdir.trim(),
        memory_enabled: memory,
      })
      onOpenChange(false)
    } catch (e) {
      // A refused working directory is shown under the field that caused it.
      if (e instanceof ApiError && e.code === "workdir") {
        setWorkdirError(e.message)
      } else {
        toastError(errorMessage(e), {
          id: "project:save",
          title: t("project.saveFailed"),
        })
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{project ? t("project.edit") : t("project.new")}</DialogTitle>
          <DialogDescription>
            {t("project.desc")}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-4">
          <div className="grid gap-1.5">
            <Label htmlFor="project-name">{t("project.name")}</Label>
            <Input
              id="project-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="project-prompt">{t("project.instruction")}</Label>
            <Textarea
              id="project-prompt"
              rows={4}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              {t("project.instructionHint")}
            </p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="project-workdir">{t("project.workdir")}</Label>
            <Input
              id="project-workdir"
              value={workdir}
              placeholder={t("project.workdirPlaceholder")}
              onChange={(e) => setWorkdir(e.target.value)}
              aria-invalid={Boolean(workdirError)}
            />
            {workdirError ? (
              <p className="text-xs text-destructive">{workdirError}</p>
            ) : (
              <p className="text-xs text-muted-foreground">
                {t("project.workdirHint")}
              </p>
            )}
          </div>

          <div className="flex items-start justify-between gap-4 rounded-md border border-border p-3">
            <div className="min-w-0">
              <Label htmlFor="project-memory">{t("project.memory")}</Label>
              <p className="mt-1 text-xs text-muted-foreground">
                {memoryAvailable
                  ? t("project.memoryOn")
                  : t("project.memoryOff")}
              </p>
              {project ? (
                <p className="mt-1 break-all text-xs text-muted-foreground">
                  {t("project.storedIn", { path: project.memory_dir })}
                </p>
              ) : null}
            </div>
            <Switch
              id="project-memory"
              checked={memory && memoryAvailable}
              disabled={!memoryAvailable}
              onCheckedChange={setMemory}
            />
          </div>
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t("project.cancel")}
          </Button>
          <Button
            disabled={saving || name.trim() === ""}
            onClick={() => void submit()}
          >
            {project ? t("project.save") : t("project.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
