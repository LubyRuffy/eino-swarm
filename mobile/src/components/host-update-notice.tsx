import { useEffect, useId, useRef, useState } from "react"
import { createPortal } from "react-dom"

import { Button } from "@/components/ui/button"
import { installAndroidBack } from "@/lib/android-back"
import { downloadStatus } from "@/lib/app-update"
import { t } from "@/lib/i18n"
import type { HostUpdateControl } from "@/lib/use-host-update"

function ConfirmHostUpdate({ label, current, version, onCancel, onConfirm }: {
  label: string
  current: string
  version: string
  onCancel: () => void
  onConfirm: () => void
}) {
  const title = useId()
  const dialog = useRef<HTMLElement>(null)
  const close = useRef(onCancel)
  close.current = onCancel
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const buttons = dialog.current?.querySelectorAll("button")
    const cancel = buttons?.[0], confirm = buttons?.[1]
    cancel?.focus()
    const removeBack = installAndroidBack(() => { close.current(); return true })
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.stopPropagation()
        close.current()
      } else if (event.key === "Tab") {
        event.preventDefault()
        const next = document.activeElement === cancel ? confirm : cancel
        next?.focus()
      }
    }
    window.addEventListener("keydown", key)
    return () => { removeBack(); window.removeEventListener("keydown", key); if (previous?.isConnected) previous.focus() }
  }, [])
  return createPortal(
    <div className="fixed inset-0 z-50 flex flex-col justify-end">
      <div className="absolute inset-0 bg-foreground/40" aria-hidden onClick={onCancel} />
      <section ref={dialog} role="dialog" aria-modal="true" aria-labelledby={title} className="relative z-10 rounded-t-2xl border border-border bg-background px-5 pt-4 pb-[env(safe-area-inset-bottom)]">
        <h2 id={title} className="wrap-break-word text-lg font-semibold">{t("hostUpdate.confirmTitle", { host: label })}</h2>
        <p className="mt-2 break-words text-sm">{t("hostUpdate.versions", { current, version })}</p>
        <p className="mt-2 text-sm text-muted-foreground">{t("hostUpdate.confirmDetail")}</p>
        <div className="mt-4 flex flex-wrap justify-end gap-2 pb-4">
          <Button type="button" variant="ghost" onClick={onCancel}>{t("hostUpdate.cancel")}</Button>
          <Button type="button" onClick={onConfirm}>{t("hostUpdate.confirmAction")}</Button>
        </div>
      </section>
    </div>, document.body,
  )
}

export function HostUpdateNotice({ host, control }: { host: string; control: HostUpdateControl }) {
  const [confirmation, setConfirmation] = useState<HostUpdateControl["offer"]>()
  const { attempt, attemptStage, canStart, check, offer } = control
  useEffect(() => {
    if (confirmation && (confirmation !== offer || !canStart)) setConfirmation(undefined)
  }, [confirmation, offer, canStart])
  if (!attempt && !offer && !check) return null

  const retryable = attemptStage === "failed" || attemptStage === "missing"
  const closable = attemptStage === "completed" || attemptStage === "pending" || attemptStage === "unconfirmed"
  const download = attemptStage === "downloading" && attempt ? downloadStatus({
    received: attempt.downloaded_bytes ?? 0, total: attempt.total_bytes ?? 0,
  }) : undefined
  const error = retryable || (!attempt && check?.status === "error")
  const dismissLabel = t(offer && !attempt ? "update.later" : "home.close")
  const status = attempt && attemptStage
    ? download?.text || attemptText(attemptStage, attempt.error)
    : offer
      ? t("hostUpdate.available", { host, current: offer.current, version: offer.version })
      : check?.status === "checking" ? t("hostUpdate.checking", { host })
        : check?.status === "current" ? t("hostUpdate.current", { host })
          : check?.status === "unsupported" ? t("hostUpdate.unsupported", { host })
            : check?.status === "error" ? check.message || t("hostUpdate.checkFailed", { host }) : ""
  return <>
    <section data-testid="host-update" className="flex shrink-0 flex-col gap-2 border-b border-border bg-card px-4 py-2">
      {attempt ? <p className="wrap-break-word text-xs font-medium text-muted-foreground">{host} · {attempt.version}</p> : null}
      <p className={`wrap-break-word text-sm ${error ? "text-destructive" : ""}`} role={error ? "alert" : "status"}>{status}</p>
      {download && download.percent != null ? <progress className="h-1 w-full" max={100} value={download.percent}
        aria-label={download.text} /> : null}
      {attempt ? (
        retryable || closable ? <div className="flex flex-wrap justify-end gap-2">
          {attemptStage !== "missing" ? <Button type="button" variant="ghost" onClick={control.dismiss}>{dismissLabel}</Button> : null}
          {retryable ? <Button type="button" disabled={!canStart} onClick={control.retry}>{t("scan.retry")}</Button> : null}
        </div> : null
      ) : offer || check ? <div className="flex flex-wrap justify-end gap-2">
        <Button type="button" variant="ghost" onClick={control.dismiss}>{dismissLabel}</Button>
        {offer ? <Button type="button" disabled={!canStart} onClick={() => setConfirmation(offer)}>{t("hostUpdate.upgrade")}</Button> : null}
        {!offer && check?.status === "error" ? <Button type="button" onClick={control.checkNow}>{t("scan.retry")}</Button> : null}
      </div> : null}
    </section>
    {confirmation && confirmation === offer && canStart ? <ConfirmHostUpdate label={host} current={confirmation.current} version={confirmation.version}
      onCancel={() => setConfirmation(undefined)} onConfirm={() => { setConfirmation(undefined); control.confirm(confirmation) }} /> : null}
  </>
}

function attemptText(stage: NonNullable<HostUpdateControl["attemptStage"]>, reason: string | undefined): string {
  switch (stage) {
    case "checking": return t("hostUpdate.preparing")
    case "downloading": return t("update.downloading")
    case "installing": return t("hostUpdate.installing")
    case "saving": return t("hostUpdate.saving")
    case "restarting": return t("hostUpdate.restarting")
    case "restoring": return t("hostUpdate.restoring")
    case "completed": return t("hostUpdate.completed")
    case "failed": return reason || t("hostUpdate.failed")
    case "missing": return t("hostUpdate.missing")
    case "sending": return t("hostUpdate.sending")
    case "pending": return reason || t("hostUpdate.pending")
    case "unconfirmed": return t("hostUpdate.pending")
  }
}
