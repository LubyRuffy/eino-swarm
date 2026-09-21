import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"

export function LinkBanner({
  reconnecting,
  error,
  onRetry,
}: {
  reconnecting?: boolean
  error?: string
  onRetry?: () => void
}) {
  if (!reconnecting && !error) return null
  return (
    <div className="flex items-center gap-2 border-b border-destructive/40 bg-destructive/10 px-4 py-2">
      <p className="min-w-0 flex-1 text-sm text-destructive" role="alert">
        {reconnecting ? t("home.reconnecting") : error}
      </p>
      {onRetry && !reconnecting ? (
        <Button variant="outline" className="h-8 shrink-0 px-3" onClick={onRetry}>
          {t("scan.retry")}
        </Button>
      ) : null}
    </div>
  )
}
