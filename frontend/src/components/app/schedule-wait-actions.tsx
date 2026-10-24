import { Play, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import { useApp } from "@/store/app"
import { useT } from "@/lib/use-t"

/** Run now / Cancel wait. The transcript notice keeps labels; the composer
 *  pin is icon-only so it matches the Codex chip. */
export function ScheduleWaitActions({
  running,
  compact,
  onRunNow,
  onCancel,
}: {
  running?: boolean
  compact?: boolean
  onRunNow: () => void
  onCancel: () => void
}) {
  const t = useT()
  const restoring = useApp((s) => s.meta?.startup === "restoring")
  return (
    <div className="flex shrink-0 items-center justify-end gap-0.5">
      {!running ? (
        <Button
          type="button"
          variant={compact ? "ghost" : "outline"}
          size={compact ? "icon-sm" : "sm"}
          className="shrink-0"
          aria-label={t("schedule.runNow")}
          disabled={restoring}
          onClick={onRunNow}
        >
          <Play />
          {compact ? null : t("schedule.runNow")}
        </Button>
      ) : null}
      <Button
        type="button"
        variant="ghost"
        size={compact ? "icon-sm" : "sm"}
        className="shrink-0"
        aria-label={t("schedule.cancel")}
        disabled={restoring}
        onClick={onCancel}
      >
        {compact ? <X /> : t("schedule.cancel")}
      </Button>
    </div>
  )
}
