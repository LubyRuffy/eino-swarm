import { useEffect, useState } from "react"
import { Maximize2, Minimize2, Search, X } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  sidebarSectionClass,
  sidebarSectionLabelClass,
} from "@/components/app/sidebar-slots"
import { api } from "@/lib/api"
import { chromeTypeClass } from "@/lib/chrome-type"
import { RUNS_IN_NEW, runsInFromSchedule } from "@/lib/schedule-dest"
import {
  cadenceFormFields,
  inboxFilteredSchedules,
  isLiveSchedule,
  scheduleHeadline,
  schedulePatchDiff,
  unreadFindings,
  type InboxFilter,
} from "@/lib/schedule-view"
import type { Schedule, ScheduleCreate, ScheduleRun } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useApp } from "@/store/app"
import { ScheduleInboxForm, type Cadence } from "./schedule-inbox-form"
import { ScheduleInboxRow } from "./schedule-inbox-row"

const FILTERS = [
  ["all", "schedule.filterAll"],
  ["active", "schedule.filterActive"],
  ["paused", "schedule.filterPaused"],
  ["completed", "schedule.filterCompleted"],
] as const

type InboxDrawer = { mode: "create" } | { mode: "edit"; id: string }

/** Sidebar control that opens the Scheduled page. A fold chevron here
 *  would lie: this is a view, not a collapsed disclosure. */
export function ScheduleInboxTrigger() {
  const t = useT()
  const unread = useApp((s) => s.scheduleUnread)
  const open = useApp((s) => s.scheduleInboxOpen)
  const openInbox = useApp((s) => s.openScheduleInbox)
  const name =
    unread > 0 ? t("sidebar.scheduledUnread", { n: unread }) : t("sidebar.scheduled")
  return (
    <section className={sidebarSectionClass}>
      <button
        type="button"
        data-testid="schedule-inbox"
        aria-current={open ? "page" : undefined}
        aria-label={name}
        onClick={() => openInbox()}
        className={cn(
          sidebarSectionLabelClass,
          "w-full gap-2 rounded-lg",
          open
            ? "bg-sidebar-accent text-sidebar-foreground"
            : "text-sidebar-foreground/60",
        )}
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

function emptyForm() {
  return {
    title: "",
    prompt: "",
    cadence: "delay" as Cadence,
    cadenceValue: "",
    projectId: "none",
    runsIn: RUNS_IN_NEW,
  }
}

function formFromRow(row: Schedule) {
  const fields = cadenceFormFields(row)
  return {
    title: row.title ?? "",
    prompt: row.prompt ?? "",
    cadence: fields.cadence,
    cadenceValue: fields.value,
    projectId: (row.project_id ?? "").trim() || "none",
    runsIn: runsInFromSchedule(row),
  }
}

/** Scheduled list in the transcript column. AppShell must not subscribe to
 *  open or a click here re-parses the conversation underneath. */
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
  const [form, setForm] = useState(emptyForm)
  const [drawer, setDrawer] = useState<InboxDrawer | null>(null)
  const [expanded, setExpanded] = useState(false)
  const [filter, setFilter] = useState<InboxFilter>("active")
  const [query, setQuery] = useState("")

  const selected =
    drawer?.mode === "edit"
      ? schedules.find((row) => row.id === drawer.id)
      : undefined
  const editing = drawer?.mode === "edit"
  const creating = drawer?.mode === "create"
  const liveEdit = Boolean(selected && isLiveSchedule(selected))
  const dirty =
    selected &&
    schedulePatchDiff(selected, {
      title: form.title,
      prompt: form.prompt,
      cadence: form.cadence,
      cadenceValue: form.cadenceValue,
    })

  useEffect(() => {
    if (!open) return
    setDrawer(null)
    setExpanded(false)
    setFilter("active")
    setQuery("")
    setForm(emptyForm())
  }, [open])

  useEffect(() => {
    if (!drawer) setExpanded(false)
  }, [drawer])

  useEffect(() => {
    if (drawer?.mode !== "edit") return
    if (!schedules.some((row) => row.id === drawer.id)) setDrawer(null)
  }, [drawer, schedules])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return
      e.preventDefault()
      if (drawer && expanded) {
        setExpanded(false)
        return
      }
      if (drawer) {
        setDrawer(null)
        return
      }
      close()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, close, drawer, expanded])

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

  const drawerKey =
    drawer?.mode === "create" ? "create" : drawer?.mode === "edit" ? drawer.id : ""
  useEffect(() => {
    if (!drawerKey) return
    const id = drawerKey === "create" ? "schedule-prompt" : "schedule-title"
    document.getElementById(id)?.focus()
  }, [drawerKey])

  if (!open) return null

  const visible = inboxFilteredSchedules(schedules, filter, query)
  const listed =
    selected && !visible.some((row) => row.id === selected.id)
      ? [selected, ...visible]
      : visible
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
      if (thread) await openThread(thread)
    })()
  }

  const openCreate = () => {
    if (creating) {
      setDrawer(null)
      return
    }
    setExpanded(false)
    setForm(emptyForm())
    setDrawer({ mode: "create" })
  }

  const openEdit = (row: Schedule) => {
    if (drawer?.mode === "edit" && drawer.id === row.id) {
      setDrawer(null)
      return
    }
    setExpanded(false)
    setForm(formFromRow(row))
    setDrawer({ mode: "edit", id: row.id })
  }

  const submit = (body: ScheduleCreate) => {
    if (creating) {
      void createSchedule(body)
      setDrawer(null)
      setForm(emptyForm())
      setFilter("active")
      return
    }
    if (!selected || !dirty) return
    void patchSchedule(selected.id, dirty)
  }

  const editActions =
    selected && isLiveSchedule(selected) ? (
      <>
        {selected.status === "active" ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => void patchSchedule(selected.id, { status: "paused" })}
          >
            {t("schedule.pause")}
          </Button>
        ) : selected.status === "paused" ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => void patchSchedule(selected.id, { status: "active" })}
          >
            {t("schedule.resume")}
          </Button>
        ) : null}
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => void runScheduleNow(selected.id)}
        >
          {t("schedule.runNow")}
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          aria-label={t("schedule.cancel")}
          onClick={() => {
            void deleteSchedule(selected.id)
            setDrawer(null)
          }}
        >
          {t("schedule.cancel")}
        </Button>
      </>
    ) : null

  return (
    <section
      data-testid="schedule-page"
      aria-labelledby="schedule-page-title"
      className="absolute inset-0 z-20 flex min-h-0 overflow-hidden bg-background"
    >
      <div
        data-testid="schedule-list-pane"
        className={
          drawer && expanded ? "hidden" : "flex min-h-0 min-w-0 flex-1 flex-col"
        }
      >
        <div className="content-column content-gutter flex min-h-0 flex-1 flex-col gap-4 py-8">
          <header className="flex shrink-0 items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              <h1
                id="schedule-page-title"
                className="text-2xl font-semibold tracking-tight"
              >
                {t("schedule.inboxTitle")}
              </h1>
              <p className="text-sm text-muted-foreground">
                {t("schedule.inboxHint")}
              </p>
            </div>
            <Button
              type="button"
              size="sm"
              data-testid="schedule-create"
              aria-expanded={creating}
              onClick={openCreate}
            >
              {t("schedule.createOpen")}
            </Button>
          </header>
          {error ? (
            <div
              role="alert"
              aria-label={t("schedule.error")}
              className="shrink-0 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive"
            >
              {error}
            </div>
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
            {FILTERS.map(([id, key]) => (
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
          {listed.length === 0 ? (
            <p className="min-h-0 flex-1 text-sm text-muted-foreground">
              {emptyText}
            </p>
          ) : (
            <ul
              data-testid="schedule-list"
              className="thin-scrollbar min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden"
            >
              {listed.map((row) => (
                <ScheduleInboxRow
                  key={row.id}
                  row={row}
                  selected={drawer?.mode === "edit" && drawer.id === row.id}
                  runs={runs.filter((r) => r.schedule_id === row.id)}
                  onSelect={() => openEdit(row)}
                  onOpenFindings={openFindings}
                />
              ))}
            </ul>
          )}
        </div>
      </div>
      {drawer ? (
        <aside
          data-testid={creating ? "schedule-create-drawer" : "schedule-edit-drawer"}
          data-expanded={expanded ? "true" : undefined}
          aria-labelledby={liveEdit ? "schedule-title" : "schedule-drawer-title"}
          className={cn(
            "flex h-full shrink-0 flex-col border-l border-border bg-card",
            expanded ? "min-w-0 flex-1" : "w-96",
          )}
        >
          <div className="flex h-12 shrink-0 items-center justify-between gap-2 border-b border-border px-3">
            {liveEdit ? (
              <Input
                id="schedule-title"
                value={form.title}
                onChange={(e) => setForm((cur) => ({ ...cur, title: e.target.value }))}
                aria-label={t("schedule.title")}
                placeholder={t("schedule.titlePlaceholder")}
                className={cn(
                  chromeTypeClass,
                  "h-8 min-w-0 flex-1 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0",
                )}
              />
            ) : (
              <h2
                id="schedule-drawer-title"
                className={cn(chromeTypeClass, "truncate")}
              >
                {creating
                  ? t("schedule.createDrawerTitle")
                  : scheduleHeadline(selected ?? { title: "", prompt: "" })}
              </h2>
            )}
            <div className="flex shrink-0 items-center gap-0.5">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                data-testid="schedule-create-expand"
                aria-expanded={expanded}
                aria-label={
                  expanded ? t("schedule.createCollapse") : t("schedule.createExpand")
                }
                onClick={() => setExpanded((on) => !on)}
              >
                {expanded ? <Minimize2 /> : <Maximize2 />}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t("schedule.createClose")}
                onClick={() => setDrawer(null)}
              >
                <X />
              </Button>
            </div>
          </div>
          <ScheduleInboxForm
            expanded={expanded}
            title={editing ? form.title : undefined}
            prompt={form.prompt}
            cadence={form.cadence}
            cadenceValue={form.cadenceValue}
            projectId={form.projectId}
            runsIn={form.runsIn}
            destinationLocked={editing}
            readOnly={editing && !liveEdit}
            canSubmit={editing ? Boolean(dirty) : undefined}
            submitLabel={editing ? t("schedule.save") : undefined}
            extraActions={editActions}
            onPrompt={(prompt) => setForm((cur) => ({ ...cur, prompt }))}
            onCadence={(cadence) => setForm((cur) => ({ ...cur, cadence }))}
            onCadenceValue={(cadenceValue) =>
              setForm((cur) => ({ ...cur, cadenceValue }))
            }
            onProjectId={(projectId) => setForm((cur) => ({ ...cur, projectId }))}
            onRunsIn={(runsIn) => setForm((cur) => ({ ...cur, runsIn }))}
            onSubmit={submit}
          />
        </aside>
      ) : null}
    </section>
  )
}
