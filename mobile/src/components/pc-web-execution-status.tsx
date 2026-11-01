import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import type { PcWebExecutionStatus as Status } from "@/lib/rpc"
import { useWebExecution } from "@/lib/use-web-execution"

/** The catalog is a PC-owned snapshot, never evidence of a live cloud grant. */
export function PcWebExecutionStatus({ status, connected, onSettings }: {
  status?: Status
  connected: boolean
  onSettings?: () => void
}) {
  const { pcMode } = useWebExecution()
  const execution = pcMode === "inherit" ? status?.execution : pcMode
  const route = !connected ? t("account.webPCOffline")
    : !status ? t("account.webPCUnknown")
    : execution === "local" ? t("account.webLocal")
    : status.cloud_account === "signed_in" ? t("account.webStatusCloud")
    : status.cloud_account === "sign_in_required" ? t("account.webPCLogin") : t("account.webPCUnavailable")
  return <div className="flex shrink-0 items-center gap-2 border-b border-border px-4 py-2 text-xs text-muted-foreground">
    <p aria-live="polite" data-testid="pc-web-status" className="min-w-0 flex-1 leading-relaxed">{t("account.webPCNext")}: {pcMode === "inherit" ? t("account.webPCFollowing") + " · " : ""}{route}</p>
    {onSettings ? <Button variant="ghost" className="h-8 shrink-0 px-2 text-xs" onClick={onSettings}>{t("account.webSettings")}</Button> : null}
  </div>
}
