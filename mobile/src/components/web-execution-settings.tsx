import { t } from "@/lib/i18n"
import { savePcWebExecution, saveWebExecution, type PcWebExecution, type WebExecution } from "@/lib/web-execution"
import { useWebExecution } from "@/lib/use-web-execution"
import { useState } from "react"

export function WebExecutionSettings() {
  const { mode, pcMode } = useWebExecution()
  const [saveError, setSaveError] = useState(false)
  const save = (action: () => void) => {
    try { action(); setSaveError(false) }
    catch { setSaveError(true) }
  }
  const controlClass = "h-10 rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
  return <section aria-label={t("account.webTools")} className="flex flex-col gap-4">
    <div className="flex flex-col gap-2">
      <label htmlFor="account-web-execution" className="text-sm font-semibold">{t("account.webDirect")}</label>
      <select id="account-web-execution" value={mode} onChange={(event) => save(() => saveWebExecution(event.target.value as WebExecution))} className={controlClass}>
        <option value="auto">{t("account.webAuto")}</option>
        <option value="cloud">{t("account.webCloud")}</option>
        <option value="off">{t("account.webOff")}</option>
      </select>
      <p className="text-xs leading-relaxed text-muted-foreground">{t("account.webHint")}</p>
    </div>
    <div className="flex flex-col gap-2">
      <label htmlFor="account-pc-web-execution" className="text-sm font-semibold">{t("account.webPC")}</label>
      <select id="account-pc-web-execution" value={pcMode} onChange={(event) => save(() => savePcWebExecution(event.target.value as PcWebExecution))} className={controlClass}>
        <option value="inherit">{t("account.webInherit")}</option>
        <option value="local">{t("account.webLocal")}</option>
        <option value="cloud">{t("account.webPCCloud")}</option>
      </select>
      <p className="text-xs leading-relaxed text-muted-foreground">{t("account.webPCHint")}</p>
    </div>
    {saveError ? <p role="alert" className="text-xs text-destructive">{t("account.webSaveFailed")}</p> : null}
  </section>
}
