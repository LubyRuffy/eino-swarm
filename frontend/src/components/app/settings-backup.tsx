import { useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { api, type BackupReport, type BackupRequest } from "@/lib/api"
import { chooseBackupFile, nativeBackupFileAvailable } from "@/lib/backup-file"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"
import { errorMessage, toastError } from "@/store/toasts"

import { Field, SettingsPage, SettingsSection } from "./settings-field"

type Source = { path: string; file?: undefined } | { file: File; path?: undefined }

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
  const fileRef = useRef<HTMLInputElement>(null)
  const desktop = nativeBackupFileAvailable()

  const run = async (work: () => Promise<void>) => {
    setBusy(true)
    setNote("")
    try {
      await work()
    } catch (error) {
      toastError(errorMessage(error))
    } finally {
      setBusy(false)
    }
  }

  const writeBackup = (body: BackupRequest, name: string) => run(async () => {
    if (desktop) {
      const output = await chooseBackupFile("save", {
        title: t("backup.saveTitle"),
        buttonText: t("backup.saveButton"),
        filename: name,
      })
      if (!output) return
      await api.backupCreate({ ...body, output })
    } else {
      downloadBlob(await api.backupDownload(body), name)
    }
    setNote(t("backup.done"))
  })

  const showReport = (next: BackupReport, picked: Source) => {
    setReport(next)
    setSource(picked)
  }

  const confirm = () => run(async () => {
    if (!report || !source || report.action === "blocked") return
    const replace = report.action === "replace-all"
    const replaceProject = report.action === "replace-project"
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
  })

  const confirmLabel = report?.action === "replace-all"
    ? t("backup.confirmAll")
    : report?.action === "replace-project"
      ? t("backup.confirmReplace")
      : t("backup.confirmAdd")
  const paths = report?.manifest.external_workdirs?.map((row) => row.path).join(", ") ?? ""

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
        {!selectedId ? <p className="px-4 pb-3 text-sm text-muted-foreground">{t("backup.noProject")}</p> : null}
        {report ? (
          <div className="flex flex-col gap-2 px-4 pb-3 text-sm">
            <p>{t("backup.counts", {
              projects: report.manifest.counts?.projects ?? 0,
              threads: report.manifest.counts?.threads ?? 0,
            })}</p>
            <p>{report.manifest.includes_secrets ? t("backup.secretsYes") : t("backup.secretsNo")}</p>
            {paths ? <p>{t("backup.external", { paths })}</p> : null}
            {report.action === "blocked" ? <p role="alert">{t("backup.conflicts")}</p> : (
              <Button type="button" disabled={busy} onClick={confirm}>{confirmLabel}</Button>
            )}
          </div>
        ) : null}
        {note ? <p className="px-4 pb-3 text-sm" role="status">{note}</p> : null}
      </SettingsSection>
    </SettingsPage>
  )
}
