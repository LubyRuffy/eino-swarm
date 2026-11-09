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
import { ModelPicker } from "@/components/app/model-picker"
import { WorkdirInput } from "@/components/app/workdir-input"
import { ApiError, type ProjectPatch } from "@/lib/api"
import { defaultNuwaModelPair } from "@/lib/nuwa-models"
import type { ModelInfo, Project } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

/** Create or edit one project: its instruction, its working directory and
 *  whether it remembers anything. */
interface ModelPair {
  plannerId: string
  plannerModel: string
  executorId: string
  executorModel: string
}

const emptyPair: ModelPair = { plannerId: "", plannerModel: "", executorId: "", executorModel: "" }

function completePair(pair: ModelPair): boolean {
  return Boolean(pair.plannerId && pair.plannerModel && pair.executorId && pair.executorModel)
}

function pairFromProject(project?: Project): ModelPair | undefined {
  const pair = {
    plannerId: project?.nuwa_planner_provider_id?.trim() ?? "",
    plannerModel: project?.nuwa_planner_model?.trim() ?? "",
    executorId: project?.nuwa_executor_provider_id?.trim() ?? "",
    executorModel: project?.nuwa_executor_model?.trim() ?? "",
  }
  return completePair(pair) ? pair : undefined
}

function pairFromCatalog(models: ModelInfo[] | undefined, hint?: { providerId?: string; model?: string }): ModelPair | undefined {
  const chosen = defaultNuwaModelPair(models ?? [], hint)
  if (!chosen.planner || !chosen.executor) return undefined
  return {
    plannerId: chosen.planner.providerId,
    plannerModel: chosen.planner.model,
    executorId: chosen.executor.providerId,
    executorModel: chosen.executor.model,
  }
}

export function ProjectDialog({
  open,
  project,
  memoryAvailable,
  models,
  plannerHint,
  onOpenChange,
  onSave,
}: {
  open: boolean
  /** Undefined creates a project; a project edits it. */
  project?: Project
  /** False when memory is off for the whole install, so the per-project
   *  switch does not promise something the app will not do. */
  memoryAvailable: boolean
  /** Ready catalog. Absent in tests that only exercise the switch. */
  models?: ModelInfo[]
  /** The open conversation's model, used only when this project has no saved pair. */
  plannerHint?: { providerId?: string; model?: string }
  onOpenChange: (open: boolean) => void
  onSave: (patch: ProjectPatch) => Promise<Project>
}) {
  const t = useT()
  const [name, setName] = useState("")
  const [prompt, setPrompt] = useState("")
  const [workdir, setWorkdir] = useState("")
  const [memory, setMemory] = useState(true)
  const [nuwa, setNuwa] = useState(false)
  const [pair, setPair] = useState<ModelPair>(emptyPair)
  const [saving, setSaving] = useState(false)
  const [choosingWorkdir, setChoosingWorkdir] = useState(false)
  const [workdirError, setWorkdirError] = useState<string>()

  useEffect(() => {
    if (!open) return
    setName(project?.name ?? "")
    setPrompt(project?.system_prompt ?? "")
    setWorkdir(project?.workdir ?? "")
    setMemory(project ? project.memory_enabled : memoryAvailable)
    setNuwa(project?.nuwa_enabled ?? false)
    setPair(pairFromProject(project) ?? emptyPair)
    setWorkdirError(undefined)
    useToasts.getState().dismiss("project:save")
  }, [open, project, memoryAvailable])

  useEffect(() => {
    if (!open || !nuwa) return
    setPair((current) => completePair(current) ? current : pairFromCatalog(models, plannerHint) ?? current)
  }, [open, nuwa, models, plannerHint])

  const submit = async () => {
    setSaving(true)
    setWorkdirError(undefined)
    useToasts.getState().dismiss("project:save")
    try {
      const patch: ProjectPatch = {
        name: name.trim(),
        workdir,
        memory_enabled: memory,
        nuwa_enabled: nuwa,
      }
      const saved = pairFromProject(project)
      const changed = !saved ||
        saved.plannerId !== pair.plannerId || saved.plannerModel !== pair.plannerModel ||
        saved.executorId !== pair.executorId || saved.executorModel !== pair.executorModel
      if (completePair(pair) && (nuwa || changed)) {
        patch.nuwa_planner_provider_id = pair.plannerId
        patch.nuwa_planner_model = pair.plannerModel
        patch.nuwa_executor_provider_id = pair.executorId
        patch.nuwa_executor_model = pair.executorModel
      }
      // A name-only save must preserve an instruction changed while open.
      if (!project || prompt !== project.system_prompt) patch.system_prompt = prompt
      await onSave(patch)
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
      <DialogContent className="max-h-[90vh] max-w-xl overflow-y-auto">
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
            <WorkdirInput
              key={project?.id ?? "new"}
              value={workdir}
              onChange={(next) => {
                setWorkdir(next)
                setWorkdirError(undefined)
              }}
              error={workdirError}
              disabled={saving}
              active={open}
              onBusyChange={setChoosingWorkdir}
            />
          </div>

          <div className="flex items-start justify-between gap-4 rounded-md border border-border p-3">
            <div className="min-w-0">
              <Label htmlFor="project-nuwa">{t("project.nuwa")}</Label>
              <p id="project-nuwa-hint" className="mt-1 text-xs text-muted-foreground">
                {t("project.nuwaHint")}
              </p>
            </div>
            <Switch
              id="project-nuwa"
              aria-describedby="project-nuwa-hint"
              checked={nuwa}
              disabled={saving}
              onCheckedChange={(checked) => {
                setNuwa(checked)
                if (!checked) return
                setPair((current) => completePair(current) ? current : pairFromCatalog(models, plannerHint) ?? current)
              }}
            />
          </div>
          {nuwa && models ? (
            <div className="grid gap-3 rounded-md border border-border p-3">
              <div className="grid gap-1.5">
                <Label htmlFor="project-nuwa-planner">{t("project.nuwaPlanner")}</Label>
                <ModelPicker
                  id="project-nuwa-planner"
                  label={t("project.nuwaPlanner")}
                  models={models}
                  providerId={pair.plannerId}
                  model={pair.plannerModel}
                  onChange={(providerId, model) => setPair((current) => ({ ...current, plannerId: providerId, plannerModel: model }))}
                />
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="project-nuwa-executor">{t("project.nuwaExecutor")}</Label>
                <ModelPicker
                  id="project-nuwa-executor"
                  label={t("project.nuwaExecutor")}
                  models={models}
                  providerId={pair.executorId}
                  model={pair.executorModel}
                  onChange={(providerId, model) => setPair((current) => ({ ...current, executorId: providerId, executorModel: model }))}
                />
              </div>
              <p className="text-xs text-muted-foreground">{t("project.nuwaModelsHint")}</p>
            </div>
          ) : null}

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
            disabled={saving || choosingWorkdir || name.trim() === ""}
            onClick={() => void submit()}
          >
            {project ? t("project.save") : t("project.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
