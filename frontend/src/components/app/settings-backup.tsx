import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { ApiError, api, type BackupProgress, type BackupReport, type BackupRequest } from "@/lib/api"
import { chooseBackupFile, nativeBackupFileAvailable } from "@/lib/backup-file"
import { useT, type Translate } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"
import { errorMessage, toastError } from "@/store/toasts"

import { Field, SettingsPage, SettingsSection } from "./settings-field"

type Source = { path: string; file?: undefined } | { file: File; path?: undefined }

type ActiveTask = { id: string; title: string }

type PendingBackup = { body: BackupRequest; name: string; tasks: ActiveTask[] }

function backupPercent(progress: BackupProgress): number | undefined {
  if (progress.total <= 0) return
  return Math.min(100, Math.round((progress.done / progress.total) * 100))
}

function backupProgressText(progress: BackupProgress, t: Translate): string {
  if (progress.phase === "files") return t("backup.progressFiles", { n: progress.done })
  if (progress.phase === "restore") return t("backup.progressRestore")
  const percent = backupPercent(progress)
  if (progress.phase === "archive" && percent !== undefined) return t("backup.progressArchive", { percent })
  return t("backup.progressSnapshot")
}

function backupActiveTasks(error: unknown): ActiveTask[] | undefined {
  if (!(error instanceof ApiError) || error.code !== "backup_busy" || error.details?.skippable !== true) return
  const raw = error.details.active
  if (!Array.isArray(raw)) return
  const tasks: ActiveTask[] = []
  for (const item of raw) {
    if (!item || typeof item !== "object") continue
    const row = item as { id?: unknown; title?: unknown }
    if (typeof row.id !== "string" || row.id === "") continue
    const title = typeof row.title === "string" && row.title.trim() !== "" ? row.title : row.id
    tasks.push({ id: row.id, title })
  }
  return tasks.length > 0 ? tasks : undefined
}

function downloadBlob(blob: Blob, name: string) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement("a")
  link.href = url
  link.download = name
  link.click()
  URL.revokeObjectURL(url)
}

export function BackupTab({ query }: { query: string }) {
  const t = useT()
  const selectedId = useProjects((s) => s.selectedId)
  const [secrets, setSecrets] = useState(true)
  const [busy, setBusy] = useState(false)
  const [report, setReport] = useState<BackupReport>()
  const [source, setSource] = useState<Source>()
  const [note, setNote] = useState("")
  const [choice, setChoice] = useState<PendingBackup>()
  const [activity, setActivity] = useState<"" | "backup" | "restore">("")
  const [progress, setProgress] = useState<BackupProgress>({ running: false, done: 0, total: 0 })
  const fileRef = useRef<HTMLInputElement>(null)
  const desktop = nativeBackupFileAvailable()

  useEffect(() => {
    if (activity === "") return
    let stop = false
    const tick = () => {
      void api.backupProgress().then((next) => {
        if (!stop && next.running && next.phase) setProgress(next)
      }).catch(() => {
        // The stage already on screen stays until this transfer finishes.
      })
    }
    const id = window.setInterval(tick, 400)
    tick()
    return () => {
      stop = true
      window.clearInterval(id)
    }
  }, [activity])

  const run = async (work: () => Promise<void>) => {
    setBusy(true)
    setNote("")
    try {
      await work()
    } catch (error) {
      if (error instanceof ApiError && error.code === "backup_busy") {
        const active = error.details?.active
        toastError(Array.isArray(active) && active.length > 0 ? t("backup.busyTasks") : t("backup.busyWait"))
        return
      }
      toastError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  const writeBackup = (body: BackupRequest, name: string) => run(async () => {
    let request = body
    if (desktop && !request.output) {
      const output = await chooseBackupFile("save", {
        title: t("backup.saveTitle"),
        buttonText: t("backup.saveButton"),
        filename: name,
      })
      if (!output) return
      request = { ...request, output }
    }
    setActivity("backup")
    setProgress({ running: true, phase: "snapshot", done: 0, total: 0 })
    try {
      let skipped = 0
      try {
        if (desktop) {
          const written = await api.backupCreate(request)
          skipped = written.skipped_symlinks ?? 0
        } else {
          const downloaded = await api.backupDownload(request)
          downloadBlob(downloaded.blob, name)
          skipped = downloaded.skippedSymlinks
        }
      } catch (error) {
        const tasks = backupActiveTasks(error)
        if (tasks) {
          setChoice({ body: request, name, tasks })
          return
        }
        throw error
      }
      setChoice(undefined)
      setNote(skipped > 0 ? t("backup.doneLinks", { n: skipped }) : t("backup.done"))
    } finally {
      setActivity("")
    }
  })

  const showReport = (next: BackupReport, picked: Source) => {
    setReport(next)
    setSource(picked)
  }

  const confirm = () => run(async () => {
    if (!report || !source || report.action === "blocked") return
    const replace = report.action === "replace-all"
    const replaceProject = report.action === "replace-project"
    setActivity("restore")
    setProgress({ running: true, phase: "restore", done: 0, total: 0 })
    try {
      if (source.path) {
        await api.backupRestore({
          path: source.path,
          confirm: true,
          replace,
          replace_project: replaceProject,
        })
      } else if (source.file) {
        await api.backupRestoreFile(source.file, { replace, replace_project: replaceProject })
      }
      setNote(t("backup.restored"))
      await useApp.getState().syncThreads()
    } finally {
      setActivity("")
    }
  })

  const confirmLabel = report?.action === "replace-all"
    ? t("backup.confirmAll")
    : report?.action === "replace-project"
      ? t("backup.confirmReplace")
      : t("backup.confirmAdd")
  const paths = report?.manifest.external_workdirs?.map((row) => row.path).join(", ") ?? ""
  const omitted = report?.manifest.omitted_threads?.map((row) => row.title || row.id).join(", ") ?? ""

  return (
    <SettingsPage title={t("backup.title")} description={t("backup.desc")}>
      <SettingsSection title={t("settings.nav.backup")}>
        <Field query={query} label={t("backup.includeSecrets")} hint={t("backup.includeSecretsHint")}>
          <Switch
            checked={secrets}
            onCheckedChange={setSecrets}
            aria-label={t("backup.includeSecrets")}
          />
        </Field>
        <div className="flex flex-wrap gap-2 px-4 py-3">
          <Button type="button" disabled={busy} onClick={() => writeBackup({ kind: "full", omit_secrets: !secrets }, "zwai.zwai-backup")}>
            {t("backup.all")}
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={busy || !selectedId}
            onClick={() => writeBackup({ kind: "project", project_id: selectedId, omit_secrets: !secrets }, "project.zwai-backup")}
          >
            {t("backup.project")}
          </Button>
          {desktop ? (
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() => run(async () => {
                const path = await chooseBackupFile("open", {
                  title: t("backup.openTitle"),
                  buttonText: t("backup.openButton"),
                })
                if (!path) return
                showReport(await api.backupPreview({ path }), { path })
              })}
            >
              {t("backup.preview")}
            </Button>
          ) : (
            <Button type="button" variant="outline" disabled={busy} onClick={() => fileRef.current?.click()}>
              {t("backup.chooseFile")}
            </Button>
          )}
          <input
            ref={fileRef}
            id="backup-file"
            aria-label={t("backup.chooseFile")}
            type="file"
            accept=".zwai-backup,application/octet-stream"
            className="sr-only"
            onChange={(event) => {
              const file = event.target.files?.[0]
              event.target.value = ""
              if (!file) return
              void run(async () => {
                showReport(await api.backupPreviewFile(file), { file })
              })
            }}
          />
        </div>
        {!selectedId ? <p className="px-4 py-3 text-(length:--chrome-font-size) text-muted-foreground">{t("backup.noProject")}</p> : null}
        {report ? (
          <div className="flex flex-col gap-2 px-4 py-3 text-(length:--chrome-font-size)">
            <p>{t("backup.counts", {
              projects: report.manifest.counts?.projects ?? 0,
              threads: report.manifest.counts?.threads ?? 0,
            })}</p>
            <p>{report.manifest.includes_secrets ? t("backup.secretsYes") : t("backup.secretsNo")}</p>
            {paths ? <p>{t("backup.external", { paths })}</p> : null}
            {omitted ? <p>{t("backup.omitted", { titles: omitted })}</p> : null}
            {report.manifest.skipped_symlinks ? <p>{t("backup.skippedLinks", { n: report.manifest.skipped_symlinks })}</p> : null}
            {report.action === "blocked" ? <p role="alert">{t("backup.conflicts")}</p> : (
              <Button type="button" disabled={busy} onClick={confirm}>{confirmLabel}</Button>
            )}
          </div>
        ) : null}
        {activity ? (
          <div className="grid gap-2 px-4 py-3">
            <div
              className="h-1 overflow-hidden rounded-full bg-muted"
              role="progressbar"
              aria-label={t("backup.progressLabel")}
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={backupPercent(progress)}
            >
              <div
                className={backupPercent(progress) === undefined ? "h-full animate-pulse bg-primary" : "h-full bg-primary transition-[width] duration-300"}
                style={backupPercent(progress) === undefined ? undefined : { width: `${backupPercent(progress)}%` }}
              />
            </div>
            <p className="text-(length:--chrome-font-size)" role="status">{backupProgressText(progress, t)}</p>
          </div>
        ) : note ? <p className="px-4 py-3 text-(length:--chrome-font-size)" role="status">{note}</p> : null}
      </SettingsSection>
      <Dialog open={choice !== undefined} onOpenChange={(open) => { if (!open) setChoice(undefined) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("backup.busyTitle")}</DialogTitle>
            <DialogDescription>{t("backup.busyBody")}</DialogDescription>
          </DialogHeader>
          <ul className="max-h-40 overflow-auto text-(length:--chrome-font-size)">
            {choice?.tasks.map((task) => <li key={task.id}>{task.title}</li>)}
          </ul>
          <DialogFooter>
            <Button type="button" variant="outline" disabled={busy} onClick={() => setChoice(undefined)}>
              {t("backup.busyCancel")}
            </Button>
            <Button
              type="button"
              disabled={busy || !choice}
              onClick={() => {
                if (!choice) return
                const next = { ...choice.body, skip_active: true }
                const name = choice.name
                setChoice(undefined)
                writeBackup(next, name)
              }}
            >
              {t("backup.busySkip")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </SettingsPage>
  )
}
