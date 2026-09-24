import { Check, Loader2, X } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  TIDY_ESTIMATE_CAP,
  TIDY_SCAN_MS,
  joinNames,
  tidyEstimateRatio,
  tidyFolded,
} from "@/lib/skill-tidy"
import type { SkillTidyReport, TidyLive, TidyLiveChange } from "@/lib/types"
import { useT, type Translate } from "@/lib/use-t"

export function SkillTidyCard({
  tidying,
  skillCount,
  report,
  error,
  live,
  onDismiss,
}: {
  tidying: boolean
  skillCount: number
  report?: SkillTidyReport
  error?: string
  live?: TidyLive
  onDismiss?: () => void
}) {
  const t = useT()
  const [step, setStep] = useState(0)
  const [ratio, setRatio] = useState(0)

  useEffect(() => {
    if (!tidying) {
      setStep(0)
      setRatio(0)
      return
    }
    setStep(0)
    setRatio(0)
    const started = Date.now()
    const tick = () => setRatio(tidyEstimateRatio(Date.now() - started, skillCount))
    tick()
    const pulse = window.setInterval(tick, 200)
    const id = window.setTimeout(() => setStep(1), TIDY_SCAN_MS)
    return () => {
      window.clearInterval(pulse)
      window.clearTimeout(id)
    }
  }, [tidying, skillCount])

  if (!tidying && !report && !error) return null

  const folded = tidyFolded(report)
  const scanned = report?.scanned ?? skillCount
  const failedText = tidyFailure(t, error || report?.err)
  const failed = Boolean(failedText)

  return (
    <div
      role="status"
      data-testid="tidy-status"
      className="grid shrink-0 gap-2 rounded-md border border-border bg-muted/40 px-2 py-2 text-xs"
    >
      <div className="flex items-start gap-2">
        <p className={`min-w-0 flex-1 font-medium ${failed && !tidying ? "text-destructive" : "text-foreground"}`}>
          {tidying
            ? t("memory.tidying")
            : failedText
              ? failedText
              : folded
                ? t("memory.tidyDone")
                : report?.reviewed
                  ? t("memory.tidyNone")
                  : t("memory.tidyEmpty")}
        </p>
        {!tidying && onDismiss ? (
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={onDismiss}
            aria-label={t("memory.tidyDismiss")}
            title={t("memory.tidyDismiss")}
          >
            <X />
          </Button>
        ) : null}
      </div>

      {tidying ? (
        <TidyProgress step={step} scanned={scanned} ratio={ratio} live={live} />
      ) : report ? (
        <TidyResult report={report} />
      ) : null}
    </div>
  )
}

function TidyProgress({
  step,
  scanned,
  ratio,
  live,
}: {
  step: number
  scanned: number
  ratio: number
  live?: TidyLive
}) {
  const t = useT()
  const labels = [t("memory.tidyScan", { n: scanned }), t("memory.tidyReview")]
  const percent = Math.round(Math.min(TIDY_ESTIMATE_CAP, ratio) * 100)
  return (
    <div className="grid gap-2">
      <div
        className="h-1 overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-label={t("memory.tidyProgress")}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
      >
        <div className="h-full bg-primary transition-[width]" style={{ width: `${percent}%` }} />
      </div>
      <ol className="grid gap-1">
        {labels.map((label, i) => {
          const current = i === step
          const done = i < step
          return (
            <li key={label} className="flex items-center gap-2 text-muted-foreground">
              {current ? (
                <Loader2 className="size-3.5 animate-spin" aria-hidden />
              ) : done ? (
                <Check className="size-3.5 text-done" aria-hidden />
              ) : (
                <span className="size-3.5" aria-hidden />
              )}
              <span className={current ? "text-foreground" : undefined}>{label}</span>
            </li>
          )
        })}
      </ol>
      <TidyStream live={live} />
    </div>
  )
}

function TidyStream({ live }: { live?: TidyLive }) {
  const t = useT()
  const scroller = useRef<HTMLDivElement>(null)
  const text = live?.text ?? ""
  const changes = live?.changes ?? []
  useEffect(() => {
    const el = scroller.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [text, changes.length])
  return (
    <div
      ref={scroller}
      data-testid="tidy-stream"
      aria-label={t("memory.tidyStream")}
      className="thin-scrollbar max-h-32 overflow-y-auto whitespace-pre-wrap break-words rounded-md bg-muted px-2 py-1.5 text-muted-foreground"
    >
      {changes.map((change, i) => (
        <p key={`${change.action}-${change.name}-${i}`}>{tidyChangeLine(t, change)}</p>
      ))}
      {text !== "" ? text : changes.length === 0 ? t("memory.tidyWaiting") : null}
    </div>
  )
}

/** A failed model call must not look like the catalog was wiped. */
function tidyLeft(report: SkillTidyReport): number {
  if (
    report.err &&
    report.after === 0 &&
    report.scanned > 0 &&
    report.deleted.length === 0 &&
    report.created.length === 0
  ) {
    return report.scanned
  }
  return report.after
}

function tidyFailure(t: Translate, raw?: string): string {
  if (!raw) return ""
  if (
    raw.includes("timeout awaiting response headers") ||
    raw.includes("no first byte")
  ) {
    return t("memory.tidyHeaderTimeout")
  }
  const line = raw.replace(/^\[NodeRunError\]\s*/, "").split("\n")[0] ?? raw
  return line.split("node path:")[0]?.trim() || line
}

function tidyChangeLine(t: Translate, change: TidyLiveChange) {
  const name = change.name || change.action
  switch (change.action) {
    case "merge":
      return t("memory.tidyActMerge", { name })
    case "delete":
      return t("memory.tidyActDelete", { name })
    case "create":
      return t("memory.tidyActCreate", { name })
    default:
      return t("memory.tidyActPatch", { name })
  }
}

function TidyResult({ report }: { report: SkillTidyReport }) {
  const t = useT()
  return (
    <div className="grid gap-2">
      <p data-testid="tidy-summary">
        {t("memory.tidySummary", {
          deleted: report.deleted.length,
          created: report.created.length,
          merged: report.merged.length,
          patched: report.patched.length,
        })}
      </p>
      <div className="flex flex-wrap gap-1" data-testid="tidy-stats">
        <Badge variant="outline">{t("memory.tidyStatScanned", { n: report.scanned })}</Badge>
        <Badge variant={report.merged.length ? "success" : "outline"}>
          {t("memory.tidyStatMerged", { n: report.merged.length })}
        </Badge>
        <Badge variant={report.deleted.length ? "warning" : "outline"}>
          {t("memory.tidyStatDeleted", { n: report.deleted.length })}
        </Badge>
        <Badge variant={report.created.length ? "success" : "outline"}>
          {t("memory.tidyStatCreated", { n: report.created.length })}
        </Badge>
        <Badge variant={report.patched.length ? "success" : "outline"}>
          {t("memory.tidyStatPatched", { n: report.patched.length })}
        </Badge>
        <Badge variant="outline">{t("memory.tidyStatLeft", { n: tidyLeft(report) })}</Badge>
      </div>
      {report.merged.length > 0 ? (
        <TidyNameList
          title={t("memory.tidyMerged")}
          items={report.merged.map((row) =>
            t("memory.tidyMergeLine", {
              dropped: joinNames(row.dropped),
              keep: row.keep,
            }),
          )}
        />
      ) : null}
      {report.deleted.length > 0 ? (
        <TidyNameList title={t("memory.tidyDeleted")} items={report.deleted} />
      ) : null}
      {report.created.length > 0 ? (
        <TidyNameList title={t("memory.tidyCreated")} items={report.created} />
      ) : null}
      {report.patched.length > 0 ? (
        <TidyNameList title={t("memory.tidyPatched")} items={report.patched} />
      ) : null}
    </div>
  )
}

function TidyNameList({ title, items }: { title: string; items: string[] }) {
  return (
    <div className="grid gap-1">
      <p className="font-medium text-foreground">{title}</p>
      <ul className="grid gap-0.5 text-muted-foreground">
        {items.map((item) => (
          <li key={item} className="break-all">
            {item}
          </li>
        ))}
      </ul>
    </div>
  )
}
