import { type FormEvent } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { useT } from "@/lib/use-t"
import type { ScheduleCreate } from "@/lib/types"
import { useProjects } from "@/store/projects"

export type Cadence = "delay" | "every" | "cron"

export function ScheduleInboxForm({
  title,
  prompt,
  cadence,
  cadenceValue,
  projectId,
  onTitle,
  onPrompt,
  onCadence,
  onCadenceValue,
  onProjectId,
  onSubmit,
}: {
  title: string
  prompt: string
  cadence: Cadence
  cadenceValue: string
  projectId: string
  onTitle: (v: string) => void
  onPrompt: (v: string) => void
  onCadence: (v: Cadence) => void
  onCadenceValue: (v: string) => void
  onProjectId: (v: string) => void
  onSubmit: (body: ScheduleCreate) => void
}) {
  const t = useT()
  const projects = useProjects((s) => s.projects)
  const cadenceLabel =
    cadence === "every"
      ? t("schedule.every")
      : cadence === "cron"
        ? t("schedule.cron")
        : t("schedule.delay")

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const body: ScheduleCreate = {
      kind: "standalone",
      title: title.trim(),
      prompt: prompt.trim(),
    }
    if (projectId !== "none") body.project_id = projectId
    if (cadence === "delay") body.delay_s = Number(cadenceValue)
    else if (cadence === "every") body.every_s = Number(cadenceValue)
    else body.cron = cadenceValue.trim()
    onSubmit(body)
  }

  return (
    <form
      className="flex shrink-0 flex-col gap-3 border-b border-border pb-4"
      onSubmit={submit}
    >
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="schedule-title">{t("schedule.title")}</Label>
        <Input
          id="schedule-title"
          value={title}
          onChange={(e) => onTitle(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="schedule-prompt">{t("schedule.prompt")}</Label>
        <Textarea
          id="schedule-prompt"
          rows={3}
          value={prompt}
          onChange={(e) => onPrompt(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="schedule-cadence">{t("schedule.cadence")}</Label>
        <Select value={cadence} onValueChange={(v) => onCadence(v as Cadence)}>
          <SelectTrigger id="schedule-cadence" aria-label={t("schedule.cadence")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="delay">{t("schedule.delay")}</SelectItem>
            <SelectItem value="every">{t("schedule.every")}</SelectItem>
            <SelectItem value="cron">{t("schedule.cron")}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="schedule-cadence-value">{cadenceLabel}</Label>
        <Input
          id="schedule-cadence-value"
          type={cadence === "cron" ? "text" : "number"}
          min={cadence === "cron" ? undefined : 1}
          value={cadenceValue}
          onChange={(e) => onCadenceValue(e.target.value)}
        />
      </div>
      <div className="flex flex-col gap-1.5">
        <Label htmlFor="schedule-project">{t("schedule.project")}</Label>
        <Select value={projectId} onValueChange={onProjectId}>
          <SelectTrigger id="schedule-project" aria-label={t("schedule.project")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="none">{t("schedule.projectNone")}</SelectItem>
            {projects.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <Button type="submit">{t("schedule.create")}</Button>
    </form>
  )
}
