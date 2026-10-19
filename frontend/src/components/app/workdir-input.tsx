import { FolderOpen, Loader2 } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useT } from "@/lib/use-t"
import { chooseDirectory, nativeDirectoryAvailable } from "@/lib/workdir"
import { errorMessage } from "@/store/toasts"

export function WorkdirInput({
  value,
  onChange,
  error,
  disabled = false,
  active = true,
  onBusyChange,
}: {
  value: string
  onChange: (value: string) => void
  error?: string
  disabled?: boolean
  active?: boolean
  onBusyChange?: (busy: boolean) => void
}) {
  const t = useT()
  const native = nativeDirectoryAvailable()
  const mounted = useRef(false)
  const revision = useRef(0)
  const picker = useRef(0)
  const latest = useRef({ value, onChange, disabled, active, onBusyChange })
  latest.current = { value, onChange, disabled, active, onBusyChange }
  const [choosing, setChoosing] = useState(false)
  const [localError, setLocalError] = useState<string>()
  const fieldError = localError ?? error

  useEffect(() => {
    mounted.current = active
    picker.current++
    setChoosing(false)
    latest.current.onBusyChange?.(false)
    return () => {
      mounted.current = false
      picker.current++
      latest.current.onBusyChange?.(false)
    }
  }, [active])

  const change = (next: string) => {
    revision.current++
    setLocalError(undefined)
    latest.current.onChange(next)
  }

  const choose = async () => {
    const request = ++picker.current
    const startedRevision = revision.current
    const startedValue = value
    const current = () =>
      mounted.current &&
      latest.current.active &&
      !latest.current.disabled &&
      picker.current === request &&
      revision.current === startedRevision &&
      latest.current.value === startedValue
    setLocalError(undefined)
    setChoosing(true)
    onBusyChange?.(true)
    try {
      const path = await chooseDirectory({
        title: t("project.workdir"),
        buttonText: t("project.workdirChoose"),
        directory: value,
      })
      // A directory chosen for an earlier dialog or before a manual edit must
      // never replace the user's newer working directory.
      if (path && current()) change(path)
    } catch (failure) {
      if (current()) {
        setLocalError(t("project.workdirChooseFailed", {
          error: errorMessage(failure),
        }))
      }
    } finally {
      if (mounted.current && picker.current === request) {
        setChoosing(false)
        latest.current.onBusyChange?.(false)
      }
    }
  }

  return (
    <>
      <div className="flex gap-2">
        <Input
          id="project-workdir"
          value={value}
          disabled={disabled}
          placeholder={t("project.workdirPlaceholder")}
          onChange={(event) => change(event.target.value)}
          aria-invalid={Boolean(fieldError)}
          aria-describedby="project-workdir-hint"
        />
        {native ? (
          <Button
            type="button"
            variant="outline"
            disabled={disabled || choosing || !active}
            aria-busy={choosing || undefined}
            onClick={() => void choose()}
          >
            {choosing
              ? <Loader2 className="animate-spin" aria-hidden />
              : <FolderOpen aria-hidden />}
            {choosing ? t("project.workdirChoosing") : t("project.workdirChoose")}
          </Button>
        ) : null}
      </div>
      <p
        id="project-workdir-hint"
        role={fieldError ? "alert" : undefined}
        className={fieldError
          ? "text-xs text-destructive"
          : "text-xs text-muted-foreground"}
      >
        {fieldError ?? t("project.workdirHint")}
      </p>
      {!native ? (
        <p className="text-xs text-muted-foreground">
          {t("project.workdirDesktopHint")}
        </p>
      ) : null}
    </>
  )
}
