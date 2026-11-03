import { t } from "@/lib/i18n"
import type { PcWebExecutionStatus as Status } from "@/lib/rpc"
import { useWebExecution } from "@/lib/use-web-execution"

export type PcWebSnapshot = {
  status?: Status
  connected: boolean
}

/** The catalog is a PC-owned snapshot, never evidence of a live cloud grant. */
export function PcWebExecutionStatus({ status, connected }: PcWebSnapshot) {
  const { pcMode } = useWebExecution()
  const execution = pcMode === "inherit" ? status?.execution : pcMode
  const route = !connected ? t("account.webPCOffline")
    : !status ? t("account.webPCUnknown")
    : execution === "local" ? t("account.webLocal")
    : status.cloud_account === "signed_in" ? t("account.webStatusCloud")
    : status.cloud_account === "sign_in_required" ? t("account.webPCLogin") : t("account.webPCUnavailable")
  return <p aria-live="polite" data-testid="pc-web-status" className="text-xs leading-relaxed text-muted-foreground">{t("account.webPCNext")}: {pcMode === "inherit" ? t("account.webPCFollowing") + " · " : ""}{route}</p>
}
