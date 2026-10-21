import { ChevronDown, ChevronRight } from "lucide-react"
import { useEffect, useMemo, useState } from "react"

import { reviewTraceMetadata } from "@/lib/review-diagnostics"
import { cloudToolSummary } from "@/lib/cloud-tool"
import { reviewNotice } from "@/lib/transcript-review"
import type { Locale } from "@/lib/i18n"
import { CopyButton } from "@/components/app/copy-button"
import { Badge } from "@/components/ui/badge"
import { Disclosure } from "@/components/ui/collapsible"
import type { TranscriptState } from "@/lib/transcript"
import type { Turn, UsageSnapshot } from "@/lib/types"
import {
  contextPercent,
  formatTokens,
  formatTurnBits,
  hasUsage,
} from "@/lib/usage"
import { useT, type Translate } from "@/lib/use-t"
import { cn, formatDuration, formatTime } from "@/lib/utils"

/** Status, cost, and the turn id — not the 800-line event dump. The dump is
 *  behind Full log; `zwai trace <id>` is still the real replay. */
export function TraceTab({
  turns,
  transcript,
  usage,
  focusedTurnId,
}: {
  turns: Turn[]
  transcript: TranscriptState
  usage?: UsageSnapshot | null
  focusedTurnId?: string
}) {
  const t = useT()
  const [logOpen, setLogOpen] = useState(false)
  const newestTurnId = transcript.turns.at(-1)?.id ?? turns.at(-1)?.id
  const latest = focusedTurnId ? turns.find((turn) => turn.id === focusedTurnId) : turns.at(-1)
  const current = focusedTurnId ? transcript.turns.find((turn) => turn.id === focusedTurnId) : transcript.turns.at(-1)
  const turnId = focusedTurnId ?? current?.id ?? latest?.id

  useEffect(() => {
    setLogOpen(Boolean(focusedTurnId))
  }, [turnId, focusedTurnId])

  const rows = useMemo(
    () => (turnId ? collectRows(transcript, turnId, t.locale) : []),
    [transcript, turnId, t.locale],
  )

  if (!turnId) {
    return (
      <p className="px-4 py-8 text-center text-xs text-muted-foreground">
        {t("panel.noTrace")}
      </p>
    )
  }

  const status = current?.status ?? latest?.status ?? ""
  const error = (current?.error || latest?.error || "").trim()
  const duration = latest?.duration_ms
    ? formatDuration(latest.duration_ms)
    : undefined

  return (
    <div className="flex flex-col gap-2 p-2">
      <div className="flex items-center gap-1 rounded-md bg-muted/60 px-2 py-1.5">
        <span className="shrink-0 text-[11px] text-muted-foreground">
          {t("panel.turn")}
        </span>
        <code className="min-w-0 flex-1 truncate font-mono text-[11px]">{turnId}</code>
        {/* The id is the whole troubleshooting story: `zwai trace <id>`. */}
        <CopyButton text={turnId} label={t("panel.copy")} />
      </div>

      <div className="flex flex-col gap-1 px-2">
        <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
          {status ? (
            <Badge
              data-testid="trace-status"
              variant={statusVariant(status)}
              className="px-1.5 py-0"
            >
              {statusLabel(status, t)}
            </Badge>
          ) : null}
          {duration ? <span>{duration}</span> : null}
          {latest?.model ? <span className="truncate">{latest.model}</span> : null}
          {latest?.reasoning_effort ? (
            <span>{t("panel.thinking", { level: latest.reasoning_effort })}</span>
          ) : null}
        </div>
        {error ? (
          <p
            data-testid="trace-error"
            className="whitespace-pre-wrap text-[12px] text-destructive"
          >
            {error}
          </p>
        ) : null}
      </div>

      <UsageSummary usage={!focusedTurnId || focusedTurnId === newestTurnId ? usage : undefined} />

      {rows.length > 0 ? (
        <Disclosure
          open={logOpen}
          onOpenChange={setLogOpen}
          testId="trace-log-toggle"
          failed={status === "error"}
          summary={
            <>
              {logOpen ? (
                <ChevronDown className="size-3.5 opacity-60" />
              ) : (
                <ChevronRight className="size-3.5 opacity-60" />
              )}
              <span>{t("panel.traceLog", { n: rows.length })}</span>
            </>
          }
        >
          <ul data-testid="trace-log" className="flex flex-col gap-0.5">
            {rows.map((row, i) => (
              <li
                key={`${row.at}-${i}`}
                className={cn(
                  "flex items-start gap-2 rounded px-2 py-1 text-[12px] hover:bg-accent/60",
                  row.details && "flex-wrap",
                  row.failed ? "text-destructive" : "",
                )}
              >
                <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                  {formatTime(row.at)}
                </span>
                <span className="shrink-0 font-medium">{row.kind}</span>
                <span className="shrink-0 text-muted-foreground">{row.agent}</span>
                <div
                  className={cn(
                    row.details ? "w-full min-w-0" : "min-w-0 flex-1 truncate",
                    row.failed ? "text-destructive" : "text-muted-foreground",
                  )}
                >
                  {row.details ? (
                    <details>
                      <summary data-testid="review-trace-details" className="cursor-pointer truncate" title={row.text.split("\n")[0]}>{row.text.split("\n")[0]}</summary>
                      {row.text.includes("\n") ? <p className="mt-1 whitespace-pre-wrap">{row.text.split("\n").slice(1).join("\n")}</p> : null}
                      <pre className="thin-scrollbar max-h-80 overflow-auto whitespace-pre-wrap break-words">{row.details}</pre>
                    </details>
                  ) : row.text}
                </div>
              </li>
            ))}
          </ul>
        </Disclosure>
      ) : null}
    </div>
  )
}

function UsageSummary({ usage }: { usage?: UsageSnapshot | null }) {
  const t = useT()
  if (!hasUsage(usage)) return null
  const used = usage.context_tokens
  const limit = usage.context_window
  const pct = contextPercent(used, limit)
  const turn = formatTurnBits(usage.turn, t.locale)
  return (
    <div
      data-testid="trace-usage"
      className="flex flex-col gap-0.5 px-2 text-[11px] text-muted-foreground"
    >
      <p>
        {pct !== undefined ? `${t("usage.contextPct", { pct })} · ` : ""}
        {limit > 0
          ? t("usage.tokensOf", {
              used: formatTokens(used),
              limit: formatTokens(limit),
            })
          : t("usage.tokens", { used: formatTokens(used) })}
      </p>
      {turn ? <p>{t("usage.turnBilled", { line: turn })}</p> : null}
      {usage.thread.calls > 0 ? (
        <p>
          {t("usage.conversation", {
            tokens: formatTokens(usage.thread.total_tokens),
            n: usage.thread.calls,
            calls: usage.thread.calls === 1 ? t("usage.call") : t("usage.calls"),
          })}
        </p>
      ) : null}
    </div>
  )
}

function statusLabel(status: string, t: Translate): string {
  if (status === "running") return t("status.running")
  if (status === "done") return t("status.done")
  if (status === "error") return t("status.failed")
  if (status === "cancelled") return t("status.cancelled")
  return status
}

function statusVariant(status: string): "warning" | "success" | "danger" | "outline" {
  if (status === "running") return "warning"
  if (status === "done") return "success"
  if (status === "error") return "danger"
  return "outline"
}

function collectRows(transcript: TranscriptState, turnId: string, locale: Locale) {
  const rows: {
    at: string
    kind: string
    agent: string
    text: string
    failed: boolean
    details?: string
  }[] = []
  for (const id of transcript.agentOrder) {
    for (const b of transcript.agents[id].blocks) {
      if (b.turnId !== turnId) continue
      rows.push({
        at: b.at,
        kind: b.cloudTool ? "builtin_tool" : b.kind === "tool" ? (b.tool?.name ?? "tool") : b.kind,
        agent: b.agentId || id,
        text: b.cloudTool ? cloudToolSummary(b.cloudTool, locale) : b.review ? (reviewNotice(b.review, locale) ?? b.text) : b.kind === "tool" ? (b.tool?.args ?? "") : b.text,
        details: b.cloudTool ? b.detail : b.review ? reviewTraceMetadata(b.review) : undefined,
        failed: b.kind === "error" || Boolean(b.tool?.failed) || Boolean(b.review?.err),
      })
    }
  }
  return rows.sort((a, b) => a.at.localeCompare(b.at))
}
