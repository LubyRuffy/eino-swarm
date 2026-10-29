import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { nativeFileSave, savePhoneFile } from "@/lib/file-save"
import { t } from "@/lib/i18n"

export function FileDownload({ name, url, mime, bytes }: {
  name: string; url: string; mime: string; bytes: Uint8Array
}) {
  const alive = useRef(true)
  const busy = useRef(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    alive.current = true
    return () => { alive.current = false }
  }, [])

  async function save() {
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
  }

  if (!nativeFileSave()) {
    return <a href={url} download={name} className="mt-4 self-start text-sm text-primary underline">{t("files.download")}</a>
  }
  return (
    <div className="mt-4 flex flex-col items-start gap-2">
      <Button type="button" variant="ghost" className="px-0 text-primary underline" disabled={saving} onClick={() => void save()}>
        {saving ? t("files.saving") : t("files.download")}
      </Button>
      {saved ? <p role="status" className="text-sm text-muted-foreground">{t("files.saved")}</p> : null}
      {failed ? <p role="alert" className="text-sm text-destructive">{t("files.saveFailed")}</p> : null}
    </div>
  )
}
