import { useEffect, useRef } from "react"
import { createPortal } from "react-dom"
import { X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { installAndroidBack } from "@/lib/android-back"
import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { MAX_TIMEOUT_SECONDS, positiveSeconds, usePCTimeouts } from "@/lib/use-pc-timeouts"

function SecondsField({ caption, label = caption, value, disabled, onChange }: {
  caption: string
  label?: string
  value: string
  disabled: boolean
  onChange: (value: string) => void
}) {
  return (
    <label className="flex flex-col gap-1 text-sm">
      <span>{caption}</span>
      <Input
        aria-label={label}
        className="text-base"
        type="number"
        min={1}
        max={MAX_TIMEOUT_SECONDS}
        step={1}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  )
}

export function PCTimeoutSheet({ link, hostLabel, onClose }: { link: RemoteLink; hostLabel: string; onClose: () => void }) {
  const state = usePCTimeouts(link)
  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    document.getElementById("pc-timeouts-close")?.focus()
    const removeBack = installAndroidBack(() => { close.current(); return true })
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") { event.stopPropagation(); close.current() } }
    window.addEventListener("keydown", escape)
    return () => { removeBack(); window.removeEventListener("keydown", escape); if (previous?.isConnected) previous.focus() }
  }, [])
  const busy = state.loading || state.saving
  return createPortal(
    <div className="fixed inset-0 z-50 flex flex-col justify-end">
      <button type="button" aria-label={t("home.close")} className="absolute inset-0 bg-foreground/40" onClick={onClose} />
      <section role="dialog" aria-modal="true" aria-labelledby="pc-timeouts-title" data-testid="pc-timeout-sheet" className="relative z-10 flex max-h-[85%] w-full min-w-0 flex-col rounded-t-2xl border border-border bg-background pb-[env(safe-area-inset-bottom)]">
        <header className="flex shrink-0 items-center justify-between px-5 py-2">
          <div className="min-w-0"><h2 id="pc-timeouts-title" className="text-lg font-semibold">{t("pcTimeouts.title")}</h2><p className="truncate text-sm text-muted-foreground">{hostLabel}</p></div>
          <Button id="pc-timeouts-close" type="button" variant="ghost" aria-label={t("home.close")} className="size-10 shrink-0 px-0" onClick={onClose}><X className="size-5" aria-hidden /></Button>
        </header>
        <form className="flex min-h-0 flex-col gap-4 overflow-y-auto px-5 pb-5" onSubmit={(event) => { event.preventDefault(); void state.save() }}>
          <p className="text-sm text-muted-foreground">{t("pcTimeouts.scope")}</p>
          {state.loading ? <p role="status">{t("pcTimeouts.loading")}</p> : null}
          {state.draft ? <>
            <SecondsField caption={t("pcTimeouts.agent")} value={state.draft.agent}
              disabled={busy || state.unsupported} onChange={state.editAgent} />
            {state.draft.providers.length === 0 ? <p className="text-sm text-muted-foreground">{t("pcTimeouts.empty")}</p> : null}
            {state.draft.providers.map((row) => {
              const provider = state.settings!.providers.find((p) => p.id === row.id)!
              const label = provider.label || provider.id
              const first = positiveSeconds(row.first), idle = positiveSeconds(row.idle)
              const edited = Number(row.first) !== provider.first_byte_timeout_seconds || Number(row.idle) !== provider.timeout_seconds
              const effective = first && idle ? first : null
              return <fieldset key={row.id} className="flex min-w-0 flex-col gap-3 rounded-xl border border-border p-3">
                <legend className="max-w-full break-words px-1 text-sm font-medium">{label}</legend>
                <SecondsField caption={t("pcTimeouts.first")} label={t("pcTimeouts.first") + " · " + label}
                  value={row.first} disabled={busy || state.unsupported}
                  onChange={(value) => state.editProvider(row.id, "first", value)} />
                <SecondsField caption={t("pcTimeouts.idle")} label={t("pcTimeouts.idle") + " · " + label}
                  value={row.idle} disabled={busy || state.unsupported}
                  onChange={(value) => state.editProvider(row.id, "idle", value)} />
                <p className="text-xs text-muted-foreground">{effective === null ? t("pcTimeouts.previewInvalid") : t(edited ? "pcTimeouts.preview" : "pcTimeouts.effective", { seconds: String(effective) })}</p>
              </fieldset>
            })}
          </> : null}
          {state.invalid ? <p role="alert" className="text-sm text-destructive">{t("pcTimeouts.invalid")}</p> : null}
          {state.error ? <p role="alert" className="text-sm text-destructive">{state.error}</p> : null}
          {state.saved ? <p role="status" className="text-sm">{t("pcTimeouts.saved")}</p> : null}
          {!state.draft && state.error && !state.unsupported ? <Button type="button" variant="outline" disabled={busy} onClick={() => void state.reload()}>{t("scan.retry")}</Button> : null}
          <Button type="submit" disabled={!state.changed || state.invalid || busy || state.unsupported}>{t(state.saving ? "pcTimeouts.saving" : "pcTimeouts.save")}</Button>
        </form>
      </section>
    </div>, document.body,
  )
}
