import { useEffect, useRef, useState } from "react"
import { Maximize2, Minimize2, MoreHorizontal, Plus, Search, X } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { api } from "@/lib/api"
import { chromeTypeClass } from "@/lib/chrome-type"
import { leaveScheduledPage } from "@/components/app/dest-rail"
import {
  inboxFilteredSchedules,
  isLiveSchedule,
  scheduleHeadline,
  schedulePatchDiff,
  unreadFindings,
  type InboxFilter,
} from "@/lib/schedule-view"
import type { ScheduleCreate, ScheduleRun } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useApp } from "@/store/app"
import { useScheduleInboxUI } from "@/store/schedule-inbox-ui"
import { ScheduleInboxForm } from "./schedule-inbox-form"
import { ScheduleInboxRow } from "./schedule-inbox-row"

const FILTERS = [
  ["active", "schedule.filterActive"],
  ["paused", "schedule.filterPaused"],
  ["completed", "schedule.filterCompleted"],
] as const

/** Wait list in the nav column. The editor lives in the task column so
 *  this scroll is not painted over the conversation. */
export function ScheduleListPane() {
  const t = useT()
  const error = useApp((s) => s.error)
  const schedules = useApp((s) => s.schedules)
  const openThread = useApp((s) => s.openThread)
  const readScheduleRun = useApp((s) => s.readScheduleRun)
  const drawer = useScheduleInboxUI((s) => s.drawer)
  const expanded = useScheduleInboxUI((s) => s.expanded)
  const filter = useScheduleInboxUI((s) => s.filter)
  const query = useScheduleInboxUI((s) => s.query)
  const runs = useScheduleInboxUI((s) => s.runs)
  const setQuery = useScheduleInboxUI((s) => s.setQuery)
  const setFilter = useScheduleInboxUI((s) => s.setFilter)
  const openCreate = useScheduleInboxUI((s) => s.openCreate)
  const openEdit = useScheduleInboxUI((s) => s.openEdit)
  const [searchOpen, setSearchOpen] = useState(false)
  const showSearch = searchOpen || query.trim().length > 0
  useEffect(() => {
    if (!searchOpen) return
    document.getElementById("schedule-search")?.focus()
  }, [searchOpen])
  const section =
    filter === "paused"
      ? t("schedule.filterPaused")
      : filter === "completed"
        ? t("schedule.filterCompleted")
        : t("schedule.sectionUpcoming")

  const creating = drawer?.mode === "create"
  const selected =
    drawer?.mode === "edit" ? schedules.find((row) => row.id === drawer.id) : undefined
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
  const hidden = Boolean(drawer && expanded)

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

  return (
    <div
      data-testid="schedule-list-pane"
      className={hidden ? "hidden" : "flex min-h-0 min-w-0 flex-1 flex-col"}
    >
      <div className="flex shrink-0 items-center justify-between gap-1 px-[var(--sidebar-list-px)] pb-1 pt-2">
        <h1
          id="schedule-page-title"
          className={cn(chromeTypeClass, "min-w-0 flex-1 truncate font-medium text-sidebar-foreground")}
        >
          {t("schedule.inboxTitle")}
        </h1>
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          data-testid="schedule-search-toggle"
          aria-expanded={showSearch}
          aria-label={t("schedule.search")}
          onClick={() => {
            if (showSearch && !query.trim()) {
              setSearchOpen(false)
              return
            }
            setSearchOpen(true)
          }}
        >
          <Search />
        </Button>
      </div>
      {showSearch ? (
        <div className="relative mx-[var(--sidebar-list-px)] mb-1 shrink-0">
          <Input
            id="schedule-search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key !== "Escape") return
              e.preventDefault()
              e.stopPropagation()
              setQuery("")
              setSearchOpen(false)
            }}
            aria-label={t("schedule.search")}
            placeholder={t("schedule.searchPlaceholder")}
            className="h-8"
          />
        </div>
      ) : null}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        data-testid="schedule-create"
        aria-expanded={creating}
        className={cn(chromeTypeClass, "mx-[var(--sidebar-list-px)] mb-1 shrink-0 justify-start gap-2 px-2")}
        style={{ height: "var(--sidebar-row-height)" }}
        onClick={() => openCreate()}
      >
        <Plus />
        {t("schedule.createOpen")}
      </Button>
      {error ? (
        <div
          role="alert"
          aria-label={t("schedule.error")}
          className="mx-[var(--sidebar-list-px)] mb-2 shrink-0 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1.5 text-sm text-destructive"
        >
          {error}
        </div>
      ) : null}
      <div className="flex shrink-0 items-center justify-between gap-1 px-[var(--sidebar-list-px)] pb-1 pt-2">
        <p
          data-testid="schedule-section"
          className={cn(chromeTypeClass, "min-w-0 truncate font-medium text-sidebar-foreground")}
        >
          {section}
        </p>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              data-testid="schedule-filter-menu"
              aria-label={t("schedule.filterBy")}
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuLabel>{t("schedule.filterBy")}</DropdownMenuLabel>
            <DropdownMenuRadioGroup
              value={filter === "all" ? "active" : filter}
              onValueChange={(value) => setFilter(value as InboxFilter)}
            >
              {FILTERS.map(([id, key]) => (
                <DropdownMenuRadioItem
                  key={id}
                  value={id}
                  data-testid={`schedule-filter-${id}`}
                >
                  {t(key)}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {listed.length === 0 ? (
        <p className={cn(chromeTypeClass, "min-h-0 flex-1 px-[var(--sidebar-row-px)] text-sidebar-foreground/60")}>
          {emptyText}
        </p>
      ) : (
        <ul
          data-testid="schedule-list"
          className="thin-scrollbar min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-[var(--sidebar-list-px)]"
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
  )
}

/** Editor for the selected wait. Fills the task column; the list stays
 *  in the nav unless Expand hides it so a long instruction can use the width. */
export function ScheduleDetail() {
  const t = useT()
  const open = useApp((s) => s.scheduleInboxOpen)
  const schedules = useApp((s) => s.schedules)
  const patchSchedule = useApp((s) => s.patchSchedule)
  const deleteSchedule = useApp((s) => s.deleteSchedule)
  const runScheduleNow = useApp((s) => s.runScheduleNow)
  const createSchedule = useApp((s) => s.createSchedule)
  const drawer = useScheduleInboxUI((s) => s.drawer)
  const expanded = useScheduleInboxUI((s) => s.expanded)
  const form = useScheduleInboxUI((s) => s.form)
  const setExpanded = useScheduleInboxUI((s) => s.setExpanded)
  const patchForm = useScheduleInboxUI((s) => s.patchForm)
  const setRuns = useScheduleInboxUI((s) => s.setRuns)
  const setFilter = useScheduleInboxUI((s) => s.setFilter)
  const closeDrawer = useScheduleInboxUI((s) => s.closeDrawer)
  const reset = useScheduleInboxUI((s) => s.reset)
  const wasOpen = useRef(false)

  const selected =
    drawer?.mode === "edit" ? schedules.find((row) => row.id === drawer.id) : undefined
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
    if (open && !wasOpen.current) reset()
    wasOpen.current = open
  }, [open, reset])

  useEffect(() => {
    if (drawer?.mode !== "edit") return
    if (!schedules.some((row) => row.id === drawer.id)) closeDrawer()
  }, [drawer, schedules, closeDrawer])

  useEffect(() => {
    if (!drawer) setExpanded(false)
  }, [drawer, setExpanded])

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
        closeDrawer()
        return
      }
      leaveScheduledPage()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, drawer, expanded, closeDrawer, setExpanded])

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
  }, [open, schedules, setRuns])

  const drawerKey =
    drawer?.mode === "create" ? "create" : drawer?.mode === "edit" ? drawer.id : ""
  useEffect(() => {
    if (!open || !drawerKey) return
    const id = drawerKey === "create" ? "schedule-prompt" : "schedule-title"
    document.getElementById(id)?.focus()
  }, [open, drawerKey])

  if (!open) return null

  const submit = (body: ScheduleCreate) => {
    if (creating) {
      void createSchedule(body)
      closeDrawer()
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
            closeDrawer()
          }}
        >
          {t("schedule.cancel")}
        </Button>
      </>
    ) : null

  if (!drawer) {
    return (
      <div className="flex min-h-0 flex-1 items-center justify-center p-6">
        <p className="max-w-sm text-center text-sm text-muted-foreground">
          {t("schedule.stageEmpty")}
        </p>
      </div>
    )
  }

  return (
    <section
      data-testid={creating ? "schedule-create-drawer" : "schedule-edit-drawer"}
      data-expanded={expanded ? "true" : undefined}
      aria-labelledby={liveEdit ? "schedule-title" : "schedule-drawer-title"}
      className="flex min-h-0 min-w-0 flex-1 flex-col bg-background"
    >
      <div className="content-column flex min-h-0 w-full flex-1 flex-col">
        <div className="flex h-12 shrink-0 items-center justify-between gap-2 border-b border-border px-3">
          {liveEdit ? (
            <Input
              id="schedule-title"
              value={form.title}
              onChange={(e) => patchForm({ title: e.target.value })}
              aria-label={t("schedule.title")}
              placeholder={t("schedule.titlePlaceholder")}
              className={cn(
                chromeTypeClass,
                "h-8 min-w-0 flex-1 border-0 bg-transparent px-0 shadow-none focus-visible:ring-0",
              )}
            />
          ) : (
            <h2 id="schedule-drawer-title" className={cn(chromeTypeClass, "truncate")}>
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
              aria-label={expanded ? t("schedule.createCollapse") : t("schedule.createExpand")}
              onClick={() => setExpanded((on) => !on)}
            >
              {expanded ? <Minimize2 /> : <Maximize2 />}
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("schedule.createClose")}
              onClick={() => closeDrawer()}
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
          onPrompt={(prompt) => patchForm({ prompt })}
          onCadence={(cadence) => patchForm({ cadence })}
          onCadenceValue={(cadenceValue) => patchForm({ cadenceValue })}
          onProjectId={(projectId) => patchForm({ projectId })}
          onRunsIn={(runsIn) => patchForm({ runsIn })}
          onSubmit={submit}
        />
      </div>
    </section>
  )
}
