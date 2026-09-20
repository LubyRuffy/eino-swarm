import { Clock } from "lucide-react"

import { ScheduleWaitActions } from "@/components/app/schedule-wait-actions"
import { WaitMark } from "@/components/app/wait-mark"
import { localizeNotice } from "@/lib/i18n"
import { scheduleHeadline } from "@/lib/schedule-view"
import type { Block } from "@/lib/transcript"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

export function isScheduleId(detail?: string): boolean {
  return Boolean(detail?.trim().startsWith("sch_"))
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

function formatWhen(iso: string): string {
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return iso
  return at.toLocaleString()
}

export function ScheduleNotice({
  block,
  onCancel,
  onRunNow,
}: {
  block: Block
  onCancel?: (id: string) => void
  onRunNow?: (id: string) => void
}) {
  const t = useT()
  const id = isScheduleId(block.detail) ? block.detail!.trim() : ""
  const running = useApp((s) => s.status.running)
  const deleteSchedule = useApp((s) => s.deleteSchedule)
  const runScheduleNow = useApp((s) => s.runScheduleNow)
  const row = useApp((s) =>
    id ? s.schedules.find((item) => item.id === id) : undefined,
  )
  if (block.quiet || !block.text) return null
  const armed = block.text === "A wait is armed." && Boolean(id)
  const headline = row ? scheduleHeadline(row) : ""
  const when = row?.next_run_at ? formatWhen(row.next_run_at) : ""
  return (
    <div
      data-testid="schedule-notice"
      className="my-2 flex items-start gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 text-[13px] text-muted-foreground"
    >
      {armed ? (
        <WaitMark className="mt-0.5 size-3.5" />
      ) : (
        <Clock className="mt-0.5 size-3.5 shrink-0" aria-hidden />
      )}
      <div className="min-w-0 flex-1">
        <p className="stream-text whitespace-pre-wrap">
          {localizeNotice(block.text, t.locale)}
        </p>
        {armed && when ? (
          <p data-testid="schedule-next" className="mt-0.5 text-xs">
            {t("schedule.nextCheck", { time: when })}
          </p>
        ) : null}
        {armed && headline ? (
          <p data-testid="schedule-prompt" className="mt-0.5 truncate text-xs">
            {headline}
          </p>
        ) : null}
      </div>
      {armed ? (
        <ScheduleWaitActions
          running={running}
          onRunNow={() => {
            if (onRunNow) onRunNow(id)
            else void runScheduleNow(id)
          }}
          onCancel={() => {
            if (onCancel) onCancel(id)
            else void deleteSchedule(id)
          }}
        />
      ) : null}
    </div>
  )
}
