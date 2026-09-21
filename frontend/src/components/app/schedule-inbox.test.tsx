import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { Sidebar } from "./sidebar"
import { ScheduleInbox } from "./schedule-inbox"
import { ScheduleInboxForm } from "./schedule-inbox-form"
import type { Schedule, ScheduleRun } from "@/lib/types"
import { runsInThreadValue } from "@/lib/schedule-dest"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

const fake = vi.hoisted(() => ({
  rows: [] as Schedule[],
  unread: 0,
  patched: [] as Array<{ id: string; patch: Record<string, unknown> }>,
  ran: [] as string[],
  created: [] as Array<Record<string, unknown>>,
  deleted: [] as string[],
  marked: [] as string[],
  runs: [] as ScheduleRun[],
  opened: [] as string[],
  runBusy: false,
}))

vi.mock("@/lib/api", () => {
  class ApiError extends Error {
    constructor(
      message: string,
      readonly status: number,
      readonly code?: string,
    ) {
      super(message)
    }
  }
  return {
    ApiError,
    api: {
    schedules: async () => ({ schedules: fake.rows, unread: fake.unread }),
    patchSchedule: async (id: string, patch: Record<string, unknown>) => {
      fake.patched.push({ id, patch })
      const row = fake.rows.find((s) => s.id === id)
      const next = { ...row, id, ...patch } as Schedule
      fake.rows = fake.rows.map((s) => (s.id === id ? next : s))
      return next
    },
    runSchedule: async (id: string) => {
      if (fake.runBusy) {
        throw new ApiError("the conversation is already running", 409, "skipped_busy")
      }
      fake.ran.push(id)
      return { id: "tn_1" }
    },
    createSchedule: async (body: Record<string, unknown>) => {
      fake.created.push(body)
      return { id: "sch_new", kind: "standalone", status: "active", ...body }
    },
    deleteSchedule: async (id: string) => {
      fake.deleted.push(id)
    },
    schedule: async (id: string) => ({
      schedule: fake.rows.find((s) => s.id === id),
      runs: fake.runs.filter((r) => r.schedule_id === id),
    }),
    markScheduleRunRead: async (rid: string) => {
      fake.marked.push(rid)
    },
    thread: async (id: string) => {
      fake.opened.push(id)
      return { thread: { id, title: "Findings" }, status: { running: false } }
    },
    turns: async () => [],
    threadLog: async () => ({ events: [], has_more: false }),
    files: async () => ({ workspace: "/tmp", files: [] }),
    followups: async () => [],
    },
  }
})

vi.mock("@/lib/stream", () => ({
  subscribeEvents: () => () => undefined,
}))

const noop = {
  onNew: vi.fn(),
  onOpen: vi.fn(),
  onRename: vi.fn(),
  onDelete: vi.fn(),
  onSearch: vi.fn(),
  onSettings: vi.fn(),
  projects: [],
  onSelectProject: vi.fn(),
  onNewProject: vi.fn(),
  onNewInProject: vi.fn(),
  onEditProject: vi.fn(),
  onDeleteProject: vi.fn(),
  onOpenSkill: vi.fn(),
  onReorder: vi.fn(),
  onReorderProjects: vi.fn(),
  onPin: vi.fn(),
}

function renderInbox() {
  return render(
    <>
      <Sidebar threads={[]} {...noop} />
      <div data-testid="composer-stage" className="relative">
        <ScheduleInbox />
      </div>
    </>,
  )
}

async function openPage() {
  fireEvent.click(screen.getByRole("button", { name: /Scheduled/ }))
  await waitFor(() => expect(screen.getByTestId("schedule-page")).toBeInTheDocument())
}

function wait(partial: Partial<Schedule> = {}): Schedule {
  return {
    id: "sch_1",
    kind: "standalone",
    origin_thread_id: "",
    thread_id: "",
    project_id: "",
    provider_id: "",
    model: "",
    title: "Periodic check",
    prompt: "Continue the wait.",
    delay_s: 0,
    every_s: 60,
    cron: "",
    status: "active",
    next_run_at: "2026-09-19T04:00:00.000Z",
    run_count: 1,
    max_runs: 0,
    created_by: "human",
    created_at: "2026-09-19T00:00:00.000Z",
    updated_at: "2026-09-19T00:00:00.000Z",
    ...partial,
  }
}

beforeEach(() => {
  fake.rows = [wait()]
  fake.unread = 3
  fake.patched = []
  fake.ran = []
  fake.created = []
  fake.deleted = []
  fake.marked = []
  fake.opened = []
  fake.runBusy = false
  fake.runs = [
    {
      id: "srun_1",
      schedule_id: "sch_1",
      thread_id: "th_findings",
      turn_id: "tn_1",
      status: "findings",
      summary: "Something changed.",
      unread: true,
      created_at: "2026-09-19T03:00:00.000Z",
      updated_at: "2026-09-19T03:00:00.000Z",
    },
  ]
  useApp.setState({
    locale: "en",
    schedules: fake.rows,
    scheduleUnread: fake.unread,
    scheduleInboxOpen: false,
    error: undefined,
    threads: [],
    activeId: undefined,
  })
  useProjects.setState({ projects: [], selectedId: undefined })
})

describe("Scheduled inbox", () => {
  it("shows an unread badge on the sidebar section", () => {
    render(<Sidebar threads={[]} {...noop} />)
    expect(screen.getByTestId("schedule-inbox")).toBeInTheDocument()
    expect(screen.getByTestId("schedule-unread")).toHaveTextContent("3")
  })

  it("is a page control, not a collapsed fold", () => {
    render(<Sidebar threads={[]} {...noop} />)
    const trigger = screen.getByRole("button", { name: /Scheduled/ })
    expect(trigger).not.toHaveAttribute("aria-haspopup")
    expect(trigger).not.toHaveAttribute("aria-expanded")
    expect(trigger.querySelector("[data-testid=section-fold]")).toBeNull()
    expect(screen.getByTestId("schedule-inbox").querySelector("[data-testid=section-fold]")).toBeNull()
  })

  it("names the trigger with unread so the badge is not only visual", () => {
    useApp.setState({ scheduleUnread: 2 })
    render(<Sidebar threads={[]} {...noop} />)
    const trigger = screen.getByRole("button", { name: /Scheduled/ })
    expect(trigger).not.toHaveAttribute("aria-haspopup")
    expect(trigger).toHaveAccessibleName(/Scheduled, 2 unread/)
  })

  it("marks the sidebar row as the current page while Scheduled is open", async () => {
    renderInbox()
    await openPage()
    expect(screen.getByTestId("schedule-inbox")).toHaveAttribute("aria-current", "page")
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument()
  })

  it("deselects the open conversation while Scheduled is the page", async () => {
    render(
      <>
        <Sidebar
          threads={[
            {
              id: "th_1",
              title: "A chat",
              project_id: "",
              provider_id: "default",
              reasoning_effort: "",
              archived: false,
              created_at: "2026-09-19T00:00:00.000Z",
              last_active_at: "2026-09-19T00:00:00.000Z",
              running: false,
            },
          ]}
          activeId="th_1"
          {...noop}
        />
        <div data-testid="composer-stage" className="relative">
          <ScheduleInbox />
        </div>
      </>,
    )
    expect(screen.getByTestId("thread-row")).toHaveAttribute("aria-current", "true")
    await openPage()
    expect(screen.getByTestId("thread-row")).not.toHaveAttribute("aria-current")
    expect(screen.getByTestId("schedule-inbox")).toHaveAttribute("aria-current", "page")
  })

  it("lists waits as a compact row and opens an editor for pause or run now", async () => {
    renderInbox()
    await openPage()
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/Periodic check/)
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/Every 1 minute/)
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/Next run now/)
    expect(screen.queryByTestId("schedule-prompt")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Pause" })).not.toBeInTheDocument()
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    expect(screen.getByTestId("schedule-edit-drawer")).toBeInTheDocument()
    expect(screen.getByTestId("schedule-row")).toHaveAttribute("data-selected", "true")
    expect(screen.getByLabelText("Title")).toHaveValue("Periodic check")
    expect(screen.getByLabelText("Task")).toHaveValue("Continue the wait.")
    expect(screen.getByLabelText("Every (seconds)")).toHaveValue(60)
    expect(screen.getByLabelText("Runs in")).toBeDisabled()
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Pause" }))
    await waitFor(() =>
      expect(fake.patched).toEqual([{ id: "sch_1", patch: { status: "paused" } }]),
    )
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Resume" })).toBeInTheDocument(),
    )
    expect(screen.getByTestId("schedule-row")).toBeInTheDocument()
    expect(screen.getByRole("tab", { name: "Active" })).toHaveAttribute(
      "aria-selected",
      "true",
    )
    fireEvent.click(screen.getByRole("button", { name: "Run now" }))
    await waitFor(() => expect(fake.ran).toEqual(["sch_1"]))
  })

  it("opens a right drawer for Create so the list stays a list", async () => {
    renderInbox()
    await openPage()
    expect(screen.queryByLabelText("Title")).not.toBeInTheDocument()
    expect(screen.queryByTestId("schedule-create-drawer")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    const drawer = screen.getByTestId("schedule-create-drawer")
    expect(drawer).toHaveTextContent("New")
    expect(drawer).toHaveTextContent("New conversation each run")
    expect(drawer).toHaveTextContent("Details")
    expect(drawer).toHaveTextContent("Frequency")
    expect(screen.queryByLabelText("Title")).not.toBeInTheDocument()
    expect(drawer).toContainElement(screen.getByLabelText("Task"))
    expect(screen.getByLabelText("Runs in")).toBeInTheDocument()
    expect(screen.getByLabelText("Repeat")).toBeInTheDocument()
    expect(screen.getByLabelText("Delay (seconds)")).toBeInTheDocument()
    expect(screen.getByLabelText("Project")).toBeInTheDocument()
    expect(screen.getByTestId("schedule-list")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Expand" })).toBeInTheDocument()
    expect(screen.getByTestId("schedule-page").textContent).not.toMatch(/0 9 \*|GitHub|deploy/i)
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    expect(screen.queryByLabelText("Task")).not.toBeInTheDocument()
    expect(screen.getByTestId("schedule-list")).toBeInTheDocument()
  })

  it("saves title, prompt, and cadence from the editor", async () => {
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Mine" } })
    fireEvent.change(screen.getByLabelText("Task"), { target: { value: "Check again." } })
    fireEvent.change(screen.getByLabelText("Every (seconds)"), { target: { value: "120" } })
    expect(screen.getByRole("button", { name: "Save" })).not.toBeDisabled()
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() =>
      expect(fake.patched).toEqual([
        {
          id: "sch_1",
          patch: { title: "Mine", prompt: "Check again.", every_s: 120 },
        },
      ]),
    )
  })

  it("keeps cadence off the PATCH when only the title changed", async () => {
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Mine" } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() =>
      expect(fake.patched).toEqual([{ id: "sch_1", patch: { title: "Mine" } }]),
    )
  })

  it("closes the editor when the same row is clicked again", async () => {
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    expect(screen.getByTestId("schedule-edit-drawer")).toBeInTheDocument()
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    expect(screen.queryByTestId("schedule-edit-drawer")).not.toBeInTheDocument()
  })

  it("closes the edit drawer on Escape before leaving the page", async () => {
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    expect(screen.getByTestId("schedule-edit-drawer")).toBeInTheDocument()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.getByTestId("schedule-page")).toBeInTheDocument()
    expect(screen.queryByTestId("schedule-edit-drawer")).not.toBeInTheDocument()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.queryByTestId("schedule-page")).not.toBeInTheDocument()
  })

  it("shows a finished wait read-only", async () => {
    fake.rows = [
      wait({
        id: "sch_done",
        title: "old",
        status: "done",
        next_run_at: "2026-09-19T01:00:00.000Z",
      }),
    ]
    fake.runs = []
    useApp.setState({ schedules: fake.rows })
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByRole("tab", { name: "Completed" }))
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    const drawer = screen.getByTestId("schedule-edit-drawer")
    expect(drawer).toHaveTextContent("old")
    expect(screen.getByLabelText("Task")).toHaveValue("Continue the wait.")
    expect(screen.getByLabelText("Task")).toHaveAttribute("readOnly")
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Run now" })).not.toBeInTheDocument()
    expect(screen.queryByLabelText("Title")).not.toBeInTheDocument()
  })

  it("submits a standalone wait from the create form", async () => {
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    fireEvent.change(screen.getByLabelText("Task"), {
      target: { value: "Continue the wait." },
    })
    fireEvent.change(screen.getByLabelText("Delay (seconds)"), { target: { value: "30" } })
    fireEvent.click(screen.getByRole("button", { name: "Add wait" }))
    await waitFor(() =>
      expect(fake.created).toEqual([
        {
          kind: "standalone",
          prompt: "Continue the wait.",
          delay_s: 30,
        },
      ]),
    )
    expect(screen.queryByLabelText("Task")).not.toBeInTheDocument()
  })

  it("wakes an existing conversation instead of minting one", () => {
    const onSubmit = vi.fn()
    render(
      <ScheduleInboxForm
        prompt="Continue the wait."
        cadence="delay"
        cadenceValue="45"
        projectId="pj_ignored"
        runsIn={runsInThreadValue("th_live")}
        onPrompt={vi.fn()}
        onCadence={vi.fn()}
        onCadenceValue={vi.fn()}
        onProjectId={vi.fn()}
        onRunsIn={vi.fn()}
        onSubmit={onSubmit}
      />,
    )
    expect(screen.queryByLabelText("Project")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Add wait" }))
    expect(onSubmit).toHaveBeenCalledWith({
      kind: "thread",
      thread_id: "th_live",
      prompt: "Continue the wait.",
      delay_s: 45,
    })
  })

  it("opens unread findings in that fire's conversation", async () => {
    renderInbox()
    await openPage()
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Open findings" })).toBeInTheDocument(),
    )
    expect(screen.queryByTestId("schedule-findings")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Open findings" }))
    await waitFor(() => expect(fake.marked).toEqual(["srun_1"]))
    await waitFor(() => expect(fake.opened).toEqual(["th_findings"]))
    await waitFor(() => expect(screen.queryByTestId("schedule-page")).not.toBeInTheDocument())
  })

  it("keeps several unread fires behind one Open findings control", async () => {
    fake.runs = [
      {
        id: "srun_old",
        schedule_id: "sch_1",
        thread_id: "th_old",
        turn_id: "tn_old",
        status: "findings",
        summary: "earlier change",
        unread: true,
        created_at: "2026-09-19T03:00:00.000Z",
        updated_at: "2026-09-19T03:00:00.000Z",
      },
      {
        id: "srun_new",
        schedule_id: "sch_1",
        thread_id: "th_new",
        turn_id: "tn_new",
        status: "findings",
        summary: "later change",
        unread: true,
        created_at: "2026-09-19T04:00:00.000Z",
        updated_at: "2026-09-19T04:00:00.000Z",
      },
    ]
    renderInbox()
    await openPage()
    expect(screen.getByTestId("schedule-page").className).toMatch(/\binset-0\b/)
    expect(screen.getByTestId("schedule-page").className).toMatch(/\boverflow-hidden\b/)
    expect(screen.getByTestId("schedule-page").className).not.toMatch(/max-h-\[85vh\]/)
    expect(screen.getByTestId("schedule-list").className).toMatch(/\boverflow-y-auto\b/)
    expect(screen.getByTestId("schedule-row")).toHaveClass("shrink-0")
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Open findings (2)" })).toBeInTheDocument(),
    )
    expect(screen.queryByRole("button", { name: "later change" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "earlier change" })).not.toBeInTheDocument()
    expect(screen.queryByTestId("schedule-findings")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Open findings (2)" }))
    await waitFor(() => expect(fake.marked).toEqual(["srun_new"]))
    await waitFor(() => expect(fake.opened).toEqual(["th_new"]))
  })

  it("marks every unread fire on the opened conversation", async () => {
    fake.runs = [
      {
        id: "srun_old",
        schedule_id: "sch_1",
        thread_id: "th_same",
        turn_id: "tn_old",
        status: "findings",
        summary: "earlier change",
        unread: true,
        created_at: "2026-09-19T03:00:00.000Z",
        updated_at: "2026-09-19T03:00:00.000Z",
      },
      {
        id: "srun_new",
        schedule_id: "sch_1",
        thread_id: "th_same",
        turn_id: "tn_new",
        status: "findings",
        summary: "later change",
        unread: true,
        created_at: "2026-09-19T04:00:00.000Z",
        updated_at: "2026-09-19T04:00:00.000Z",
      },
    ]
    renderInbox()
    await openPage()
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Open findings (2)" })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole("button", { name: "Open findings (2)" }))
    await waitFor(() => expect(fake.marked).toEqual(["srun_new", "srun_old"]))
    await waitFor(() => expect(fake.opened).toEqual(["th_same"]))
  })

  it("shows skipped_busy on the scheduled page", async () => {
    fake.runBusy = true
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByTestId("schedule-row-toggle"))
    fireEvent.click(screen.getByRole("button", { name: "Run now" }))
    const alert = await waitFor(() => screen.getByRole("alert"))
    expect(alert).toHaveTextContent("The conversation is already running a turn.")
    expect(alert).toHaveAccessibleName(/error/i)
  })

  it("defaults to Active and reveals ended waits on Completed", async () => {
    fake.rows = [
      wait({
        id: "sch_done",
        title: "old",
        status: "done",
        next_run_at: "2026-09-19T01:00:00.000Z",
      }),
      wait({
        id: "sch_live",
        title: "",
        status: "active",
        next_run_at: "2026-09-19T04:00:00.000Z",
      }),
    ]
    fake.runs = []
    useApp.setState({ schedules: fake.rows })
    renderInbox()
    await openPage()
    const rows = screen.getAllByTestId("schedule-row")
    expect(rows).toHaveLength(1)
    expect(rows[0].textContent).toMatch(/Continue the wait/)
    expect(screen.queryByText("old")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("tab", { name: "Completed" }))
    const revealed = screen.getAllByTestId("schedule-row")
    expect(revealed).toHaveLength(1)
    expect(revealed[0].textContent).toMatch(/old/)
    fireEvent.click(screen.getByRole("tab", { name: "All" }))
    expect(screen.getAllByTestId("schedule-row")).toHaveLength(2)
  })

  it("keeps an ended wait with unread findings on Completed, not Active", async () => {
    fake.rows = [
      wait({
        id: "sch_done",
        title: "old",
        status: "done",
        next_run_at: "2026-09-19T01:00:00.000Z",
      }),
    ]
    fake.runs = [
      {
        id: "srun_1",
        schedule_id: "sch_done",
        thread_id: "th_findings",
        turn_id: "tn_1",
        status: "findings",
        summary: "Something changed.",
        unread: true,
        created_at: "2026-09-19T03:00:00.000Z",
        updated_at: "2026-09-19T03:00:00.000Z",
      },
    ]
    useApp.setState({ schedules: fake.rows })
    renderInbox()
    await openPage()
    expect(screen.queryByTestId("schedule-row")).not.toBeInTheDocument()
    expect(screen.getByText("No waits in this view.")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("tab", { name: "Completed" }))
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/old/)
    expect(screen.getByRole("button", { name: "Open findings" })).toBeInTheDocument()
    expect(screen.queryByTestId("schedule-findings")).not.toBeInTheDocument()
  })

  it("says this view is empty when every row has ended", async () => {
    fake.rows = [
      wait({
        id: "sch_done",
        title: "old",
        status: "done",
        next_run_at: "2026-09-19T01:00:00.000Z",
      }),
    ]
    fake.runs = []
    useApp.setState({ schedules: fake.rows })
    renderInbox()
    await openPage()
    expect(screen.getByText("No waits in this view.")).toBeInTheDocument()
    expect(screen.queryByTestId("schedule-row")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("tab", { name: "Completed" }))
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/old/)
  })

  it("filters the list from the search box", async () => {
    fake.rows = [
      wait({ id: "sch_keep", title: "alpha wait", prompt: "Continue the wait." }),
      wait({ id: "sch_drop", title: "other wait", prompt: "Stay parked." }),
    ]
    fake.runs = []
    useApp.setState({ schedules: fake.rows })
    renderInbox()
    await openPage()
    await waitFor(() => expect(screen.getAllByTestId("schedule-row")).toHaveLength(2))
    fireEvent.change(screen.getByLabelText("Search waits"), { target: { value: "alpha" } })
    expect(screen.getAllByTestId("schedule-row")).toHaveLength(1)
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/alpha wait/)
    fireEvent.change(screen.getByLabelText("Search waits"), { target: { value: "zzzz" } })
    expect(screen.getByText("No matching waits.")).toBeInTheDocument()
    expect(screen.queryByTestId("schedule-row")).not.toBeInTheDocument()
  })

  it("closes the create drawer on Escape before leaving the page", async () => {
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    expect(screen.getByTestId("schedule-create-drawer")).toBeInTheDocument()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.getByTestId("schedule-page")).toBeInTheDocument()
    expect(screen.queryByTestId("schedule-create-drawer")).not.toBeInTheDocument()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.queryByTestId("schedule-page")).not.toBeInTheDocument()
  })

  it("expands the create drawer over the list so a long task is readable", async () => {
    renderInbox()
    await openPage()
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    const drawer = screen.getByTestId("schedule-create-drawer")
    expect(drawer).not.toHaveAttribute("data-expanded")
    expect(screen.getByTestId("schedule-list-pane")).not.toHaveClass("hidden")
    fireEvent.click(screen.getByRole("button", { name: "Expand" }))
    expect(drawer).toHaveAttribute("data-expanded", "true")
    expect(screen.getByRole("button", { name: "Collapse" })).toHaveAttribute(
      "aria-expanded",
      "true",
    )
    expect(screen.getByTestId("schedule-list-pane")).toHaveClass("hidden")
    expect(screen.getByLabelText("Task")).toBeInTheDocument()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(drawer).toBeInTheDocument()
    expect(drawer).not.toHaveAttribute("data-expanded")
    expect(screen.getByTestId("schedule-list-pane")).not.toHaveClass("hidden")
    fireEvent.click(screen.getByRole("button", { name: "Expand" }))
    fireEvent.click(screen.getByRole("button", { name: "Collapse" }))
    expect(drawer).not.toHaveAttribute("data-expanded")
    fireEvent.click(screen.getByRole("button", { name: "Expand" }))
    fireEvent.click(screen.getByRole("button", { name: "Close" }))
    fireEvent.click(screen.getByRole("button", { name: "Create" }))
    expect(screen.getByTestId("schedule-create-drawer")).not.toHaveAttribute(
      "data-expanded",
    )
  })

  it("leaves the page on Escape", async () => {
    renderInbox()
    await openPage()
    fireEvent.keyDown(window, { key: "Escape" })
    expect(screen.queryByTestId("schedule-page")).not.toBeInTheDocument()
    expect(screen.getByTestId("schedule-inbox")).not.toHaveAttribute("aria-current")
  })
})
