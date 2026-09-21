import { Check, Loader2, X } from "lucide-react"
import { useEffect, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  TIDY_PROGRESS_STEPS,
  TIDY_STEP_MS,
  joinNames,
  tidyFolded,
} from "@/lib/skill-tidy"
import type { SkillTidyReport } from "@/lib/types"
import { useT } from "@/lib/use-t"

export function SkillTidyCard({
  tidying,
  skillCount,
  report,
  error,
  onDismiss,
}: {
  tidying: boolean
  skillCount: number
  report?: SkillTidyReport
  error?: string
  onDismiss?: () => void
}) {
  const t = useT()
  const [step, setStep] = useState(0)
  const [holding, setHolding] = useState(false)

  useEffect(() => {
    if (!tidying) return
    setStep(0)
    setHolding(true)
  }, [tidying])

  useEffect(() => {
    if (tidying) return
    if (step >= TIDY_PROGRESS_STEPS.length - 1) setHolding(false)
  }, [tidying, step])

  useEffect(() => {
    if (!tidying && !holding) return
    if (step >= TIDY_PROGRESS_STEPS.length - 1 && !tidying) return
    const id = window.setInterval(() => {
      setStep((current) => Math.min(current + 1, TIDY_PROGRESS_STEPS.length - 1))
    }, TIDY_STEP_MS)
    return () => window.clearInterval(id)
  }, [tidying, holding, step])

  const showProgress = tidying || holding
  if (!showProgress && !report && !error) return null

  const folded = tidyFolded(report)
  const scanned = report?.scanned ?? skillCount

  return (
    <div
      role="status"
      data-testid="tidy-status"
      className="grid shrink-0 gap-2 rounded-md border border-border bg-muted/40 px-2 py-2 text-xs"
    >
      <div className="flex items-start gap-2">
        <p className={`min-w-0 flex-1 font-medium ${error && !showProgress ? "text-destructive" : "text-foreground"}`}>
          {showProgress
            ? t("memory.tidying")
            : error
              ? error
              : folded
                ? t("memory.tidyDone")
                : t("memory.tidyNone")}
        </p>
        {!showProgress && onDismiss ? (
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

      {showProgress ? (
        <TidyProgress step={step} scanned={scanned} spinning={tidying} />
      ) : error ? null : report ? (
        <TidyResult report={report} />
      ) : null}
    </div>
  )
}

function TidyProgress({
  step,
  scanned,
  spinning,
}: {
  step: number
  scanned: number
  spinning: boolean
}) {
  const t = useT()
  const labels = [
    t("memory.tidyScan", { n: scanned }),
    t("memory.tidyGroup"),
    t("memory.tidyFold"),
  ]
  const width = step === 0 ? "w-1/3" : step === 1 ? "w-2/3" : "w-full"
  return (
    <div className="grid gap-2">
      <div
        className="h-1 overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-label={t("memory.tidyProgress")}
        aria-valuemin={0}
        aria-valuemax={TIDY_PROGRESS_STEPS.length}
        aria-valuenow={step + 1}
      >
        <div className={`h-full bg-primary transition-[width] ${width}`} />
      </div>
      <ol className="grid gap-1">
        {labels.map((label, i) => {
          const last = i === labels.length - 1
          const current = i === step
          const done = i < step || (last && current && !spinning)
          const showSpin = current && (spinning || !last)
          return (
            <li key={label} className="flex items-center gap-2 text-muted-foreground">
              {showSpin && !done ? (
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
    </div>
  )
}

function TidyResult({ report }: { report: SkillTidyReport }) {
  const t = useT()
  return (
    <div className="grid gap-2">
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
        <Badge variant="outline">{t("memory.tidyStatLeft", { n: report.after })}</Badge>
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
