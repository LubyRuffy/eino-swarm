import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { Sidebar } from "./sidebar"
import type { Schedule, ScheduleRun } from "@/lib/types"
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
      return { ...row, id, ...patch }
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

  it("is a dialog trigger, not a collapsed fold", () => {
    render(<Sidebar threads={[]} {...noop} />)
    const trigger = screen.getByRole("button", { name: /Scheduled/ })
    expect(trigger).toHaveAttribute("aria-haspopup", "dialog")
    expect(trigger).toHaveAttribute("aria-expanded", "false")
    expect(trigger.querySelector("[data-testid=section-fold]")).toBeNull()
    expect(screen.getByTestId("schedule-inbox").querySelector("[data-testid=section-fold]")).toBeNull()
  })

  it("names the trigger with unread so the badge is not only visual", () => {
    useApp.setState({ scheduleUnread: 2 })
    render(<Sidebar threads={[]} {...noop} />)
    const trigger = screen.getByRole("button", { name: /Scheduled/ })
    expect(trigger).toHaveAttribute("aria-haspopup", "dialog")
    expect(trigger).toHaveAccessibleName(/Scheduled, 2 unread/)
  })

  it("marks the trigger expanded while the inbox dialog is open", async () => {
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: /Scheduled/ }))
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument())
    // Modal inert hides the trigger from the a11y tree; the attribute still
    // has to flip so a screen reader that inspects the control is not lied to.
    expect(screen.getByTestId("schedule-inbox")).toHaveAttribute("aria-expanded", "true")
  })

  it("lists waits and can pause or run now", async () => {
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: /Scheduled/ }))
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument())
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/Periodic check/)
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/Standalone/)
    expect(screen.getByTestId("schedule-row").textContent).toMatch(/Active/)
    expect(screen.getByTestId("schedule-prompt").textContent).toMatch(/Continue the wait/)
    fireEvent.click(screen.getByRole("button", { name: "Pause" }))
    await waitFor(() =>
      expect(fake.patched).toEqual([{ id: "sch_1", patch: { status: "paused" } }]),
    )
    fireEvent.click(screen.getByRole("button", { name: "Run now" }))
    await waitFor(() => expect(fake.ran).toEqual(["sch_1"]))
  })

  it("labels the create form so getByLabel works", async () => {
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: /Scheduled/ }))
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument())
    expect(screen.getByLabelText("Title")).toBeInTheDocument()
    expect(screen.getByLabelText("Prompt")).toBeInTheDocument()
    expect(screen.getByLabelText("Cadence")).toBeInTheDocument()
    expect(screen.getByLabelText("Delay (seconds)")).toBeInTheDocument()
    expect(screen.getByLabelText("Project")).toBeInTheDocument()
    expect(screen.getByRole("dialog").textContent).not.toMatch(/0 9 \*|GitHub|deploy/i)
  })

  it("opens unread findings in that fire's conversation", async () => {
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: /Scheduled/ }))
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument())
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Open findings" })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole("button", { name: "Open findings" }))
    await waitFor(() => expect(fake.marked).toEqual(["srun_1"]))
    await waitFor(() => expect(fake.opened).toEqual(["th_findings"]))
  })

  it("shows skipped_busy inside the inbox dialog", async () => {
    fake.runBusy = true
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: /Scheduled/ }))
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument())
    fireEvent.click(screen.getByRole("button", { name: "Run now" }))
    const alert = await waitFor(() => screen.getByRole("alert"))
    expect(alert).toHaveTextContent("The conversation is already running a turn.")
    expect(alert).toHaveAccessibleName(/error/i)
  })

  it("puts a live untitled wait above done rows and shows its prompt", async () => {
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
    useApp.setState({ schedules: fake.rows })
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: /Scheduled/ }))
    await waitFor(() => expect(screen.getByRole("dialog")).toBeInTheDocument())
    const rows = screen.getAllByTestId("schedule-row")
    expect(rows[0].textContent).toMatch(/Continue the wait/)
    expect(rows[0].textContent).toMatch(/Active/)
    expect(rows[1].textContent).toMatch(/old/)
  })
})
