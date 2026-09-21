import { useEffect, useState } from "react"
import { Search } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"
import { useT } from "@/lib/use-t"
import type { ScheduleCreate, ScheduleRun } from "@/lib/types"
import {
  inboxFilteredSchedules,
  unreadFindings,
  type InboxFilter,
} from "@/lib/schedule-view"
import { useApp } from "@/store/app"
import { ScheduleInboxForm, type Cadence } from "./schedule-inbox-form"
import { ScheduleInboxRow } from "./schedule-inbox-row"

/** Sidebar control that opens the inbox. A fold chevron here would lie:
 *  this is a dialog trigger, not a collapsed disclosure. */
export function ScheduleInboxTrigger() {
  const t = useT()
  const unread = useApp((s) => s.scheduleUnread)
  const open = useApp((s) => s.scheduleInboxOpen)
  const openInbox = useApp((s) => s.openScheduleInbox)
  const name =
    unread > 0 ? t("sidebar.scheduledUnread", { n: unread }) : t("sidebar.scheduled")
  return (
    <section className="mb-2">
      <button
        type="button"
        data-testid="schedule-inbox"
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={name}
        onClick={() => openInbox()}
        className="flex h-7 w-full items-center gap-2 px-2 text-left text-[11px] font-medium uppercase tracking-wide text-sidebar-foreground/60"
      >
        <span className="min-w-0 flex-1 truncate" aria-hidden="true">
          {t("sidebar.scheduled")}
        </span>
        {unread > 0 ? (
          <Badge data-testid="schedule-unread" aria-hidden="true">
            {unread}
          </Badge>
        ) : null}
      </button>
    </section>
  )
}

/** Inbox dialog. The sidebar owns mount + open; App must not re-render for it. */
export function ScheduleInbox() {
  const t = useT()
  const open = useApp((s) => s.scheduleInboxOpen)
  const close = useApp((s) => s.closeScheduleInbox)
  const error = useApp((s) => s.error)
  const schedules = useApp((s) => s.schedules)
  const patchSchedule = useApp((s) => s.patchSchedule)
  const deleteSchedule = useApp((s) => s.deleteSchedule)
  const runScheduleNow = useApp((s) => s.runScheduleNow)
  const createSchedule = useApp((s) => s.createSchedule)
  const readScheduleRun = useApp((s) => s.readScheduleRun)
  const openThread = useApp((s) => s.openThread)
  const [runs, setRuns] = useState<ScheduleRun[]>([])
  const [title, setTitle] = useState("")
  const [prompt, setPrompt] = useState("")
  const [cadence, setCadence] = useState<Cadence>("delay")
  const [cadenceValue, setCadenceValue] = useState("")
  const [projectId, setProjectId] = useState("none")
  const [creating, setCreating] = useState(false)
  const [filter, setFilter] = useState<InboxFilter>("active")
  const [query, setQuery] = useState("")

  useEffect(() => {
    if (!open) return
    setCreating(false)
    setFilter("active")
    setQuery("")
    setTitle("")
    setPrompt("")
    setCadence("delay")
    setCadenceValue("")
    setProjectId("none")
  }, [open])

  useEffect(() => {
    if (!open) return
    let gone = false
    Promise.all(schedules.map((row) => api.schedule(row.id)))
      .then((details) => {
        if (!gone) setRuns(details.flatMap((d) => d.runs ?? []))
      })
      .catch(() => undefined)
    return () => {
      gone = true
    }
  }, [open, schedules])

  if (!open) return null

  const visible = inboxFilteredSchedules(schedules, filter, query)
  const emptyText =
    schedules.length === 0
      ? t("schedule.empty")
      : query.trim()
        ? t("schedule.emptySearch")
        : t("schedule.emptyFilter")

  const openFindings = (run: ScheduleRun) => {
    void (async () => {
      const thread = (run.thread_id ?? "").trim()
      const same = thread
        ? unreadFindings(runs).filter((r) => (r.thread_id ?? "").trim() === thread)
        : []
      const toRead = same.length > 0 ? same : [run]
      for (const r of toRead) await readScheduleRun(r.id)
      close()
      if (thread) await openThread(thread)
    })()
  }

  const submit = (body: ScheduleCreate) => {
    void createSchedule(body)
    setCreating(false)
    setTitle("")
    setPrompt("")
    setCadence("delay")
    setCadenceValue("")
    setProjectId("none")
    setFilter("active")
  }

  return (
    <Dialog open onOpenChange={(next) => { if (!next) close() }}>
      <DialogContent className="flex max-h-[85vh] max-w-2xl flex-col overflow-x-hidden overflow-hidden">
        <DialogHeader className="flex flex-row items-start justify-between gap-3 pr-6">
          <div className="min-w-0 flex-1">
            <DialogTitle>{t("schedule.inboxTitle")}</DialogTitle>
            <DialogDescription>{t("schedule.inboxHint")}</DialogDescription>
          </div>
          <Button
            type="button"
            size="sm"
            data-testid="schedule-create"
            aria-expanded={creating}
            onClick={() => setCreating((on) => !on)}
          >
            {t("schedule.createOpen")}
          </Button>
        </DialogHeader>
        {error ? (
          <div
            role="alert"
            aria-label={t("schedule.error")}
            className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
          >
            {error}
          </div>
        ) : null}
        {creating ? (
          <ScheduleInboxForm
            title={title}
            prompt={prompt}
            cadence={cadence}
            cadenceValue={cadenceValue}
            projectId={projectId}
            onTitle={setTitle}
            onPrompt={setPrompt}
            onCadence={setCadence}
            onCadenceValue={setCadenceValue}
            onProjectId={setProjectId}
            onSubmit={submit}
          />
        ) : null}
        <div className="relative shrink-0">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            id="schedule-search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label={t("schedule.search")}
            placeholder={t("schedule.searchPlaceholder")}
            className="pl-8"
          />
        </div>
        <div
          role="tablist"
          aria-label={t("schedule.filter")}
          className="flex shrink-0 flex-wrap gap-1"
        >
          {(
            [
              ["all", "schedule.filterAll"],
              ["active", "schedule.filterActive"],
              ["paused", "schedule.filterPaused"],
              ["completed", "schedule.filterCompleted"],
            ] as const
          ).map(([id, key]) => (
            <Button
              key={id}
              type="button"
              role="tab"
              size="sm"
              variant={filter === id ? "secondary" : "ghost"}
              aria-selected={filter === id}
              data-testid={`schedule-filter-${id}`}
              className="rounded-full"
              onClick={() => setFilter(id)}
            >
              {t(key)}
            </Button>
          ))}
        </div>
        {visible.length === 0 ? (
          <p className="text-sm text-muted-foreground">{emptyText}</p>
        ) : (
          <ul
            data-testid="schedule-list"
            className="thin-scrollbar min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden"
          >
            {visible.map((row) => (
              <ScheduleInboxRow
                key={row.id}
                row={row}
                runs={runs.filter((r) => r.schedule_id === row.id)}
                onPause={() => void patchSchedule(row.id, { status: "paused" })}
                onResume={() => void patchSchedule(row.id, { status: "active" })}
                onCancel={() => void deleteSchedule(row.id)}
                onRunNow={() => void runScheduleNow(row.id)}
                onOpenFindings={openFindings}
              />
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  )
}
