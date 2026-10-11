import { useEffect, useLayoutEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { closeClient } from "@/lib/client-open"
import { setDeskDest } from "@/lib/desk-nav"
import { chromeTypeClass } from "@/lib/chrome-type"
import {
  conversationSchedules,
  scheduleHeadline,
} from "@/lib/schedule-view"
import type { Schedule } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useApp } from "@/store/app"
import { useScheduleInboxUI } from "@/store/schedule-inbox-ui"
import { scheduleMetaLine } from "./schedule-inbox-row"

function scheduleLinkKey(rows: Schedule[]): string {
  return rows
    .map((row) =>
      [row.id, row.kind, row.thread_id, row.status, String(row.run_count)].join("\t"),
    )
    .sort()
    .join("\n")
}

function openConversationSchedule(row: Schedule) {
  setDeskDest("scheduled")
  closeClient()
  useScheduleInboxUI.getState().showEdit(row)
  useApp.getState().openScheduleInbox()
}

/** Waits aimed at this conversation, and the job that minted it.
 *  Sits above the rail tabs so it stays visible without a tab change. */
export function ConversationSchedules({ threadId }: { threadId?: string }) {
  const t = useT()
  const schedules = useApp((s) => s.schedules)
  const linkKey = scheduleLinkKey(schedules)
  const [createdBy, setCreatedBy] = useState<string[]>([])
  const id = (threadId ?? "").trim()

  // Drop the previous chat's minted-job ids before paint. The fetch below
  // fills the new list; leaving the old ids up would show another chat's job.
  useLayoutEffect(() => {
    setCreatedBy([])
  }, [id])

  useEffect(() => {
    if (!id) return
    let gone = false
    api
      .threadSchedules(id)
      .then((got) => {
        if (!gone) setCreatedBy(got.created_by ?? [])
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [id, linkKey])
  const rows = id ? conversationSchedules(schedules, id, createdBy) : []
  if (rows.length === 0) return null

  return (
    <section
      data-testid="conversation-schedules"
      aria-label={t("schedule.inboxTitle")}
      className="shrink-0 border-b border-border"
    >
      <h2 className={cn(chromeTypeClass, "px-3 pt-2 font-medium text-muted-foreground")}>
        {t("schedule.inboxTitle")}
      </h2>
      <ul className="thin-scrollbar max-h-40 overflow-y-auto px-1 pb-1">
        {rows.map((row) => {
          const headline = scheduleHeadline(row) || row.id
          const meta = scheduleMetaLine(row, t)
          return (
            <li key={row.id}>
              <Button
                type="button"
                variant="ghost"
                data-testid="conversation-schedule"
                data-schedule-id={row.id}
                className="h-auto w-full justify-start px-2 py-1.5 text-left"
                onClick={() => openConversationSchedule(row)}
              >
                <span className="block w-full min-w-0">
                  <span className="block truncate text-sm font-medium">{headline}</span>
                  {meta ? (
                    <span className="block truncate text-xs text-muted-foreground">{meta}</span>
                  ) : null}
                </span>
              </Button>
            </li>
          )
        })}
      </ul>
    </section>
  )
}
