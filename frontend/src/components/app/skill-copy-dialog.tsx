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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api } from "@/lib/api"
import type { Project } from "@/lib/types"
import { useT } from "@/lib/use-t"

/** Copy is a snapshot into another project. The dialog says so because a
 *  shared live skill is how one project's edits become everyone's. */
export function SkillCopyDialog({
  open,
  sourceId,
  names,
  fromLibrary,
  onOpenChange,
  onCopied,
}: {
  open: boolean
  sourceId: string
  /** Absent means every skill in the source project. */
  names?: string[]
  /** The shared library is not a project. Copy goes through its own endpoint. */
  fromLibrary?: boolean
  onOpenChange: (open: boolean) => void
  onCopied: (message: string) => void
}) {
  const t = useT()
  const [dests, setDests] = useState<Project[]>([])
  const [loaded, setLoaded] = useState(false)
  const [to, setTo] = useState("")
  const [asName, setAsName] = useState("")
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState(false)
  const single = names?.length === 1

  useEffect(() => {
    if (!open) return
    let cancelled = false
    setError(undefined)
    setAsName("")
    setTo("")
    setBusy(false)
    setLoaded(false)
    void api.projects().then(
      (list) => {
        if (cancelled) return
        const next = list.filter((p) => p.id !== sourceId)
        setDests(next)
        if (next.length === 1) setTo(next[0].id)
        setLoaded(true)
      },
      (e) => {
        if (cancelled) return
        setError(e instanceof Error ? e.message : String(e))
        setLoaded(true)
      },
    )
    return () => {
      cancelled = true
    }
  }, [open, sourceId])

  if (!open) return null

  const dest = dests.find((p) => p.id === to)
  const title = single
    ? t("skill.copyTitle", { name: names?.[0] ?? "" })
    : t("skill.copyAllTitle")

  const submit = async () => {
    if (!to) return
    setBusy(true)
    setError(undefined)
    try {
      const result = fromLibrary
        ? await api.copyLibrarySkills({
            to_project: to,
            ...(names && names.length > 0 ? { names } : {}),
            ...(single && asName.trim() ? { as: asName.trim() } : {}),
          })
        : await api.copySkills(sourceId, {
            to_project: to,
            ...(names && names.length > 0 ? { names } : {}),
            ...(single && asName.trim() ? { as: asName.trim() } : {}),
          })
      if (result.copied.length === 0) {
        setError(
          result.skipped
            .map((row) => t("skill.copySkipped", { name: row.name, error: row.error }))
            .join("\n") || t("skill.copyFailed"),
        )
        return
      }
      const skipped =
        result.skipped.length > 0
          ? ` ${result.skipped
              .map((row) => t("skill.copySkipped", { name: row.name, error: row.error }))
              .join(" ")}`
          : ""
      onCopied(
        t("skill.copied", { project: dest?.name ?? to, n: result.copied.length }) + skipped,
      )
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{t("skill.copyDesc")}</DialogDescription>
        </DialogHeader>
        {loaded && dests.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("skill.copyNone")}</p>
        ) : (
          <div className="grid gap-3">
            <div className="grid gap-1.5">
              <Label htmlFor="skill-copy-to">{t("skill.copyTo")}</Label>
              <Select value={to} onValueChange={setTo} disabled={!loaded || busy}>
                <SelectTrigger id="skill-copy-to">
                  <SelectValue placeholder={loaded ? t("skill.copyPick") : t("memory.loading")}>
                    {dest?.name}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {dests.map((project) => (
                    <SelectItem key={project.id} value={project.id}>
                      {project.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            {single ? (
              <div className="grid gap-1.5">
                <Label htmlFor="skill-copy-as">{t("skill.copyAs")}</Label>
                <Input
                  id="skill-copy-as"
                  value={asName}
                  onChange={(e) => setAsName(e.target.value)}
                  placeholder={names?.[0]}
                  autoComplete="off"
                  disabled={busy}
                />
                <p className="text-xs text-muted-foreground">{t("skill.copyAsHint")}</p>
              </div>
            ) : null}
          </div>
        )}
        {error ? (
          <p role="alert" className="whitespace-pre-wrap text-sm text-destructive">
            {error}
          </p>
        ) : null}
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={busy}>
            {t("confirm.cancel")}
          </Button>
          <Button
            type="button"
            onClick={() => void submit()}
            disabled={!to || busy || dests.length === 0}
          >
            {busy ? t("skill.copyWorking") : single ? t("skill.copyConfirm") : t("skill.copyAllConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** A pull that would replace local edits. The source file is not written. */
export function SkillPullDialog({
  open,
  onOpenChange,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
}) {
  const t = useT()
  if (!open) return null
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t("skill.pullConfirmTitle")}</DialogTitle>
          <DialogDescription>{t("skill.pullConfirmDesc")}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
            {t("confirm.cancel")}
          </Button>
          <Button
            type="button"
            onClick={() => {
              onConfirm()
              onOpenChange(false)
            }}
          >
            {t("skill.pullConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
