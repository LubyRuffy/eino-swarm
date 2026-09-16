import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import {
  contextPercent,
  formatTokens,
  formatTurnBits,
  hasUsage,
  meterFill,
} from "@/lib/usage"
import type { UsageSnapshot } from "@/lib/types"
import { useT } from "@/lib/use-t"

/** Cursor-style context ring. The number is the last manager prompt; the
 *  window is whichever model is selected now, so switching models changes
 *  the percentage without waiting for another call. */
export function ContextMeter({
  usage,
  window,
  scale = 0,
}: {
  usage?: UsageSnapshot | null
  window: number
  /** Compact character budget. Visual half-life when `window` is unknown. */
  scale?: number
}) {
  const t = useT()
  if (!hasUsage(usage)) return null
  const used = usage.context_tokens
  const limit = window > 0 ? window : usage.context_window
  const pct = contextPercent(used, limit)
  const fill = meterFill(used, limit, scale)
  const tone =
    pct === undefined
      ? "text-muted-foreground"
      : pct >= 95
        ? "text-destructive"
        : pct >= 80
          ? "text-running"
          : "text-foreground"
  const label =
    pct === undefined
      ? t("usage.tokensUsed", { used: formatTokens(used) })
      : t("usage.contextPct", { pct })

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          data-testid="context-meter"
          aria-label={label}
          className="flex size-7 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground"
        >
          <Ring className={tone} fill={fill} />
        </button>
      </TooltipTrigger>
      <TooltipContent
        side="top"
        align="end"
        className="max-w-56 border border-border bg-popover px-3 py-2 text-popover-foreground"
      >
        <p className="text-sm font-medium">{label}</p>
        {limit > 0 ? (
          <p className="text-xs text-muted-foreground">
            {t("usage.tokensOf", {
              used: formatTokens(used),
              limit: formatTokens(limit),
            })}
          </p>
        ) : (
          <p className="text-xs text-muted-foreground">
            {t("usage.tokens", { used: formatTokens(used) })}
          </p>
        )}
        {pct === undefined ? (
          <p className="text-xs text-muted-foreground">
            {t("usage.setWindow")}
          </p>
        ) : null}
        <TurnLine usage={usage} locale={t.locale} />
      </TooltipContent>
    </Tooltip>
  )
}

function TurnLine({
  usage,
  locale,
}: {
  usage: UsageSnapshot
  locale: "en" | "zh"
}) {
  const t = useT()
  const line = formatTurnBits(usage.turn, locale)
  if (!line) return null
  return (
    <p className="mt-1.5 text-xs text-muted-foreground">
      {t("usage.turnBilled", { line })}
    </p>
  )
}

function Ring({ className, fill }: { className: string; fill: number }) {
  const size = 18
  const stroke = 2
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const clamped = Math.max(0, Math.min(1, fill))
  return (
    <svg
      width={size}
      height={size}
      viewBox={`0 0 ${size} ${size}`}
      className={className}
      data-fill={clamped.toFixed(2)}
      aria-hidden
    >
      <circle
        cx={size / 2}
        cy={size / 2}
        r={r}
        fill="none"
        className="stroke-border"
        strokeWidth={stroke}
      />
      <circle
        cx={size / 2}
        cy={size / 2}
        r={r}
        fill="none"
        stroke="currentColor"
        strokeWidth={stroke}
        strokeLinecap="round"
        strokeDasharray={c}
        strokeDashoffset={c * (1 - clamped)}
        transform={`rotate(-90 ${size / 2} ${size / 2})`}
      />
    </svg>
  )
}
