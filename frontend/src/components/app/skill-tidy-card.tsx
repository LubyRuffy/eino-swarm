import { Check, Loader2, X } from "lucide-react"
import { useEffect, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  TIDY_PROGRESS_STEPS,
  TIDY_SCAN_MS,
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

  useEffect(() => {
    if (!tidying) {
      setStep(0)
      return
    }
    setStep(0)
    const id = window.setTimeout(() => setStep(1), TIDY_SCAN_MS)
    return () => window.clearTimeout(id)
  }, [tidying])

  if (!tidying && !report && !error) return null

  const folded = tidyFolded(report)
  const scanned = report?.scanned ?? skillCount
  const failed = Boolean(error || report?.err)

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
            : error || report?.err
              ? error || report?.err
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
        <TidyProgress step={step} scanned={scanned} />
      ) : report ? (
        <TidyResult report={report} />
      ) : null}
    </div>
  )
}

function TidyProgress({ step, scanned }: { step: number; scanned: number }) {
  const t = useT()
  const labels = [t("memory.tidyScan", { n: scanned }), t("memory.tidyReview")]
  const width = step === 0 ? "w-1/2" : "w-full"
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
        <Badge variant={report.patched.length ? "success" : "outline"}>
          {t("memory.tidyStatPatched", { n: report.patched.length })}
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
