import { Clock } from "lucide-react"

import { Button } from "@/components/ui/button"
import { localizeNotice } from "@/lib/i18n"
import type { Block } from "@/lib/transcript"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

export function isScheduleId(detail?: string): boolean {
  return Boolean(detail?.startsWith("sch_"))
}

/** Armed / cancelled / fired chips. CompactNotice would treat sch_… as a briefing. */
export function isScheduleNotice(block: Block): boolean {
  if (isScheduleId(block.detail)) return true
  return (
    block.text === "A wait is armed." ||
    block.text === "A wait was cancelled." ||
    block.text === "Scheduled check."
  )
}

export function ScheduleNotice({
  block,
  onCancel,
}: {
  block: Block
  onCancel?: (id: string) => void
}) {
  const t = useT()
  const deleteSchedule = useApp((s) => s.deleteSchedule)
  if (block.quiet || !block.text) return null
  const id = isScheduleId(block.detail) ? block.detail!.trim() : ""
  const armed = block.text === "A wait is armed." && Boolean(id)
  return (
    <div
      data-testid="schedule-notice"
      className="my-2 flex items-start gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground"
    >
      <Clock className="mt-0.5 size-3.5 shrink-0" />
      <p className="min-w-0 flex-1 stream-text whitespace-pre-wrap">
        {localizeNotice(block.text, t.locale)}
      </p>
      {armed ? (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="-mt-0.5 shrink-0"
          aria-label={t("schedule.cancel")}
          onClick={() => {
            if (onCancel) onCancel(id)
            else void deleteSchedule(id)
          }}
        >
          {t("schedule.cancel")}
        </Button>
      ) : null}
    </div>
  )
}
