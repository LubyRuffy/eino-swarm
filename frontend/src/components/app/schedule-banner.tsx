import { Clock, X } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { useT } from "@/lib/use-t"
import type { Schedule } from "@/lib/types"
import { activeWake } from "@/store/app-schedule"
import { useApp } from "@/store/app"

function formatWhen(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString()
}

/** Same density as GoalBanner: next check + cancel, only for an active wake. */
export function ScheduleBanner({
  wake: wakeProp,
  onCancel,
}: {
  wake?: Schedule
  onCancel?: () => void
}) {
  const t = useT()
  const schedules = useApp((s) => s.schedules)
  const activeId = useApp((s) => s.activeId)
  const deleteSchedule = useApp((s) => s.deleteSchedule)
  const wake = wakeProp ?? activeWake(schedules, activeId)
  if (!wake || wake.kind !== "thread" || wake.status !== "active") return null
  const cancel = onCancel ?? (() => void deleteSchedule(wake.id))
  const when = wake.next_run_at ? formatWhen(wake.next_run_at) : ""

  return (
    <div data-testid="schedule-banner" className="mb-2 rounded-xl border bg-card px-3 py-2">
      <div className="flex items-start gap-2">
        <Clock className="mt-1 size-3.5 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline">{t("schedule.waiting")}</Badge>
            {when ? (
              <span data-testid="schedule-next" className="text-xs text-muted-foreground">
                {t("schedule.nextCheck", { time: when })}
              </span>
            ) : (
              <span data-testid="schedule-next" className="text-xs text-muted-foreground">
                {wake.title}
              </span>
            )}
          </div>
          {wake.title ? (
            <p className="mt-1 truncate text-sm">{wake.title}</p>
          ) : null}
        </div>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={t("schedule.cancel")}
          onClick={cancel}
        >
          <X />
        </Button>
      </div>
    </div>
  )
}
