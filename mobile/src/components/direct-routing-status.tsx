import { Button } from "@/components/ui/button"
import type { DirectProvider } from "@/lib/direct-provider"
import { t } from "@/lib/i18n"
import type { WebExecution } from "@/lib/web-execution"

export function DirectRoutingStatus({ provider, mode, cloudSignedIn, onSettings }: {
  provider?: DirectProvider
  mode: WebExecution
  cloudSignedIn: boolean
  onSettings?: () => void
}) {
  const web = mode === "off" ? t("account.webStatusOff")
    : cloudSignedIn ? t("account.webStatusCloud")
    : mode === "cloud" ? t("account.webStatusRequired") : t("account.webStatusAnonymous")
  const source = !provider ? t("account.sourceUnavailable")
    : provider.sourceKind === "gateway" ? provider.label : t("account.personalSource", { name: provider.label || provider.id })
  return <div className="flex shrink-0 items-start gap-2 border-b border-border px-4 py-2 text-xs text-muted-foreground">
    <div className="min-w-0 flex-1 leading-relaxed">
      <p data-testid="direct-model-source" className="break-words">{t("account.modelSource")}: {source}</p>
      <p data-testid="direct-web-status" aria-live="polite">{t("account.webTools")}: {web}</p>
      {provider && provider.sourceKind !== "gateway" && mode !== "off" ? <p>{t("account.webModelSupport")}</p> : null}
    </div>
    {onSettings ? <Button variant="ghost" className="h-8 shrink-0 px-2 text-xs" onClick={onSettings}>{t("account.webSettings")}</Button> : null}
  </div>
}
