import { useCallback, useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { nativeFileSave, savePhoneFile } from "@/lib/file-save"
import { t } from "@/lib/i18n"

type DownloadFile = { url: string; mime: string; bytes: Uint8Array }

export function FileDownload({ name, file }: { name: string; file: DownloadFile | null }) {
  const native = nativeFileSave()
  const downloadLink = useRef<HTMLAnchorElement>(null)
  const [queued, setQueued] = useState(false)
  const alive = useRef(true)
  const busy = useRef(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    alive.current = true
    return () => { alive.current = false }
  }, [])

  const save = useCallback(async ({ mime, bytes }: DownloadFile) => {
    if (busy.current) return
    busy.current = true
    setSaving(true)
    setSaved(false)
    setFailed(false)
    try {
      const completed = await savePhoneFile(name, mime, bytes)
      if (alive.current) setSaved(completed)
    } catch {
      if (alive.current) setFailed(true)
    } finally {
      busy.current = false
      if (alive.current) setSaving(false)
    }
  }, [name])

  useEffect(() => {
    if (!queued || !file) return
    setQueued(false)
    if (native) void save(file)
    else downloadLink.current?.click()
  }, [file, native, queued, save])

  if (file && !native) {
    return <a ref={downloadLink} href={file.url} download={name} hidden={queued}
      className="mt-4 self-start text-sm text-primary underline">{t("files.download")}</a>
  }
  return (
    <div className="mt-4 flex flex-col items-start gap-2">
      <Button type="button" variant="ghost" className="px-0 text-primary underline" disabled={saving || queued}
        onClick={() => file ? void save(file) : setQueued(true)}>
        {saving ? t("files.saving") : t("files.download")}
      </Button>
      {queued && !file ? <p role="status" className="text-sm text-muted-foreground">{t("files.downloadQueued")}</p> : null}
      {saved ? <p role="status" className="text-sm text-muted-foreground">{t("files.saved")}</p> : null}
      {failed ? <p role="alert" className="text-sm text-destructive">{t("files.saveFailed")}</p> : null}
    </div>
  )
}
