import { useId, useState } from "react"

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
import { api } from "@/lib/api"
import type { ExternalSkillInfo } from "@/lib/types"
import { useT } from "@/lib/use-t"

export function SkillInstallDialog({
  projectId,
  onInstalled,
  onClose,
}: {
  projectId: string
  onInstalled: (skill: ExternalSkillInfo) => void
  onClose: () => void
}) {
  const t = useT()
  const fileId = useId()
  const [file, setFile] = useState<File>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string>()

  const install = async () => {
    if (!file || busy) return
    setBusy(true)
    setError(undefined)
    try {
      onInstalled(await api.installExternalSkill(projectId, file))
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose() }}>
      <DialogContent className="max-w-md" hideClose={busy}>
        <DialogHeader>
          <DialogTitle>{t("skill.install")}</DialogTitle>
          <DialogDescription>{t("skill.installTrust")}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-1.5">
          <Label htmlFor={fileId}>{t("skill.installFile")}</Label>
          <Input
            id={fileId}
            type="file"
            accept=".zip,application/zip"
            disabled={busy}
            onChange={(e) => {
              setFile(e.currentTarget.files?.[0])
              setError(undefined)
            }}
          />
        </div>
        {error ? <p role="alert" className="break-words text-sm text-destructive">{error}</p> : null}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose} disabled={busy}>
            {t("confirm.cancel")}
          </Button>
          <Button type="button" onClick={() => void install()} disabled={!file || busy} aria-busy={busy}>
            {t(busy ? "skill.installBusy" : "skill.installConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
