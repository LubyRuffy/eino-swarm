import { Play } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useT } from "@/lib/use-t"

/** Labeled Run now / Cancel wait. An icon-only X sat under the goal
 *  banner's dismiss and people would not touch it. */
export function ScheduleWaitActions({
  running,
  onRunNow,
  onCancel,
}: {
  running?: boolean
  onRunNow: () => void
  onCancel: () => void
}) {
  const t = useT()
  return (
    <div className="flex shrink-0 flex-wrap items-center justify-end gap-1">
      {!running ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="shrink-0"
          aria-label={t("schedule.runNow")}
          onClick={onRunNow}
        >
          <Play />
          {t("schedule.runNow")}
        </Button>
      ) : null}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="shrink-0"
        aria-label={t("schedule.cancel")}
        onClick={onCancel}
      >
        {t("schedule.cancel")}
      </Button>
    </div>
  )
}
