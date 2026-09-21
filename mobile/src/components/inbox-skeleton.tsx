import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"

import { Button } from "@/components/ui/button"

const ROWS = ["w-full", "w-5/6", "w-full", "w-2/3", "w-4/5", "w-full", "w-3/4"] as const

export function InboxSkeleton({
  pending,
  error,
  onRetry,
}: {
  pending?: boolean
  error?: string
  onRetry?: () => void
}) {
  const spinning = Boolean(pending) || !error
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {spinning ? (
        <div
          role="status"
          aria-live="polite"
          aria-busy="true"
          className="flex flex-col gap-3 px-4 py-4"
        >
          <span className="sr-only">{error ? t("home.reconnecting") : t("scan.connecting")}</span>
          {ROWS.map((w, i) => (
            <div
              key={i}
              className={cn(
                "h-12 animate-pulse rounded-xl bg-muted motion-reduce:animate-none",
                w,
              )}
            />
          ))}
        </div>
      ) : null}
      {error ? (
        <div className="flex flex-col items-center gap-3 px-4 py-6">
          <p className="max-w-sm text-center text-sm text-destructive" role="alert">
            {error}
          </p>
          {onRetry && !spinning ? (
            <Button onClick={onRetry}>{t("scan.retry")}</Button>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}
