import { useEffect, useState, type FormEvent } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
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
import { api } from "@/lib/api"
import { useT } from "@/lib/use-t"
import type { Schedule, ScheduleCreate, ScheduleRun } from "@/lib/types"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

type Cadence = "delay" | "every" | "cron"

function formatWhen(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString()
}

/** Inbox dialog. The sidebar owns mount + open; App must not re-render for it. */
export function ScheduleInbox() {
  const t = useT()
  const open = useApp((s) => s.scheduleInboxOpen)
  const close = useApp((s) => s.closeScheduleInbox)
  const schedules = useApp((s) => s.schedules)
  const patchSchedule = useApp((s) => s.patchSchedule)
  const deleteSchedule = useApp((s) => s.deleteSchedule)
  const runScheduleNow = useApp((s) => s.runScheduleNow)
  const createSchedule = useApp((s) => s.createSchedule)
  const readScheduleRun = useApp((s) => s.readScheduleRun)
  const openThread = useApp((s) => s.openThread)
  const projects = useProjects((s) => s.projects)
  const [runs, setRuns] = useState<ScheduleRun[]>([])
  const [title, setTitle] = useState("")
  const [prompt, setPrompt] = useState("")
  const [cadence, setCadence] = useState<Cadence>("delay")
  const [cadenceValue, setCadenceValue] = useState("")
  const [projectId, setProjectId] = useState("none")

  useEffect(() => {
    if (!open) return
    let gone = false
    Promise.all(schedules.map((row) => api.schedule(row.id)))
      .then((details) => {
        if (!gone) setRuns(details.flatMap((d) => d.runs ?? []))
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [open, schedules])

  if (!open) return null

  const cadenceLabel =
    cadence === "every" ? t("schedule.every") : cadence === "cron" ? t("schedule.cron") : t("schedule.delay")

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
    void createSchedule(body)
  }

  const openFindings = (run: ScheduleRun) => {
    void (async () => {
      await readScheduleRun(run.id)
      close()
      if (run.thread_id) await openThread(run.thread_id)
    })()
  }

  return (
    <Dialog open onOpenChange={(next) => { if (!next) close() }}>
      <DialogContent className="max-h-[80vh] max-w-2xl overflow-auto">
        <DialogHeader>
          <DialogTitle>{t("schedule.inboxTitle")}</DialogTitle>
          <DialogDescription>{t("schedule.inboxHint")}</DialogDescription>
        </DialogHeader>
        {schedules.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("schedule.empty")}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {schedules.map((row) => (
              <ScheduleRow
                key={row.id}
                row={row}
                runs={runs.filter((r) => r.schedule_id === row.id)}
                onPause={() => void patchSchedule(row.id, { status: "paused" })}
                onResume={() => void patchSchedule(row.id, { status: "active" })}
                onCancel={() => void deleteSchedule(row.id)}
                onRunNow={() => void runScheduleNow(row.id)}
                onOpenFindings={openFindings}
              />
            ))}
          </ul>
        )}
        <form className="flex flex-col gap-3 border-t border-border pt-4" onSubmit={submit}>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="schedule-title">{t("schedule.title")}</Label>
            <Input
              id="schedule-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="schedule-prompt">{t("schedule.prompt")}</Label>
            <Textarea
              id="schedule-prompt"
              rows={3}
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="schedule-cadence">{t("schedule.cadence")}</Label>
            <Select value={cadence} onValueChange={(v) => setCadence(v as Cadence)}>
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
              onChange={(e) => setCadenceValue(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="schedule-project">{t("schedule.project")}</Label>
            <Select value={projectId} onValueChange={setProjectId}>
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
      </DialogContent>
    </Dialog>
  )
}

function ScheduleRow({
  row,
  runs,
  onPause,
  onResume,
  onCancel,
  onRunNow,
  onOpenFindings,
}: {
  row: Schedule
  runs: ScheduleRun[]
  onPause: () => void
  onResume: () => void
  onCancel: () => void
  onRunNow: () => void
  onOpenFindings: (run: ScheduleRun) => void
}) {
  const t = useT()
  const kind =
    row.kind === "thread" ? t("schedule.kindThread") : t("schedule.kindStandalone")
  const status =
    row.status === "paused"
      ? t("schedule.statusPaused")
      : row.status === "done"
        ? t("schedule.statusDone")
        : row.status === "cancelled"
          ? t("schedule.statusCancelled")
          : t("schedule.statusActive")
  const findings = runs.filter(
    (r) => r.unread && (r.status === "findings" || r.status === "error") && r.thread_id,
  )
  return (
    <li
      data-testid="schedule-row"
      className="rounded-lg border border-border bg-card px-3 py-2"
    >
      <div className="flex flex-wrap items-center gap-2">
        <p className="min-w-0 flex-1 truncate text-sm font-medium">{row.title}</p>
        <Badge variant="outline">{kind}</Badge>
        <Badge variant={row.status === "active" ? "success" : "outline"}>{status}</Badge>
      </div>
      {row.next_run_at ? (
        <p className="mt-1 text-xs text-muted-foreground">{formatWhen(row.next_run_at)}</p>
      ) : null}
      <div className="mt-2 flex flex-wrap gap-1">
        {row.status === "active" ? (
          <Button type="button" variant="ghost" size="sm" onClick={onPause}>
            {t("schedule.pause")}
          </Button>
        ) : row.status === "paused" ? (
          <Button type="button" variant="ghost" size="sm" onClick={onResume}>
            {t("schedule.resume")}
          </Button>
        ) : null}
        {row.status === "active" || row.status === "paused" ? (
          <>
            <Button type="button" variant="ghost" size="sm" onClick={onRunNow}>
              {t("schedule.runNow")}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              aria-label={t("schedule.cancel")}
              onClick={onCancel}
            >
              {t("schedule.cancel")}
            </Button>
          </>
        ) : null}
        {findings.map((run) => (
          <Button
            key={run.id}
            type="button"
            variant="secondary"
            size="sm"
            onClick={() => onOpenFindings(run)}
          >
            {t("schedule.openFindings")}
          </Button>
        ))}
      </div>
    </li>
  )
}
