import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { ScheduleDetail } from "./schedule-inbox"
import { ConversationSchedules } from "./conversation-schedules"
import { setDeskDest } from "@/lib/desk-nav"
import type { Schedule } from "@/lib/types"
import { useApp } from "@/store/app"
import { resetScheduleInboxUI, useScheduleInboxUI } from "@/store/schedule-inbox-ui"

const fake = vi.hoisted(() => ({
  createdBy: [] as string[],
  rows: [] as Schedule[],
}))

vi.mock("@/lib/api", () => ({
  api: {
    threadSchedules: async () => ({ schedules: fake.rows, created_by: fake.createdBy }),
    schedule: async () => ({ schedule: {}, runs: [] }),
    schedules: async () => ({ schedules: fake.rows, unread: 0 }),
  },
}))

function row(partial: Partial<Schedule> & { id: string }): Schedule {
  return {
    kind: "thread",
    origin_thread_id: "th_home",
    thread_id: "th_home",
    project_id: "",
    provider_id: "",
    model: "",
    title: "",
    prompt: "Continue the wait.",
    delay_s: 0,
    every_s: 3600,
    cron: "",
    status: "active",
    next_run_at: "2026-10-07T02:00:00.000Z",
    run_count: 0,
    max_runs: 0,
    created_by: "human",
    created_at: "2026-10-07T00:00:00.000Z",
    updated_at: "2026-10-07T00:00:00.000Z",
    ...partial,
  }
}

beforeEach(() => {
  fake.createdBy = []
  fake.rows = []
  setDeskDest("chats")
  resetScheduleInboxUI()
  useApp.setState({
    locale: "en",
    schedules: [],
    scheduleInboxOpen: false,
    activeId: "th_home",
    error: undefined,
  })
})

describe("conversation schedules", () => {
  it("lists a bound wait and the job that created the chat, then opens that job", async () => {
    fake.createdBy = ["sch_mint"]
    fake.rows = [
      row({ id: "sch_bound", title: "Aimed here" }),
      row({
        id: "sch_mint",
        kind: "standalone",
        thread_id: "",
        title: "Created this chat",
        status: "cancelled",
      }),
      row({ id: "sch_else", thread_id: "th_other", title: "Aimed elsewhere" }),
      row({ id: "sch_dead", status: "cancelled", title: "Cancelled wake" }),
    ]
    useApp.setState({ schedules: fake.rows })
    render(
      <>
        <ConversationSchedules threadId="th_home" />
        <ScheduleDetail />
      </>,
    )
    const list = await screen.findByTestId("conversation-schedules")
    expect(list).toHaveTextContent("Aimed here")
    await waitFor(() => expect(list).toHaveTextContent("Created this chat"))
    expect(list).not.toHaveTextContent("Aimed elsewhere")
    expect(list).not.toHaveTextContent("Cancelled wake")

    fireEvent.click(screen.getByRole("button", { name: /Created this chat/ }))
    expect(useApp.getState().scheduleInboxOpen).toBe(true)
    expect(useScheduleInboxUI.getState().drawer).toEqual({ mode: "edit", id: "sch_mint" })
    expect(await screen.findByRole("heading", { name: "Created this chat" })).toBeInTheDocument()
  })

  it("drops the previous conversation's minted job before the next list arrives", async () => {
    let release: (value: { schedules: Schedule[]; created_by: string[] }) => void = () => undefined
    fake.createdBy = ["sch_mint"]
    fake.rows = [
      row({
        id: "sch_mint",
        kind: "standalone",
        thread_id: "",
        title: "Created this chat",
        status: "cancelled",
      }),
    ]
    useApp.setState({ schedules: fake.rows })
    const api = await import("@/lib/api")
    const pending = vi.spyOn(api.api, "threadSchedules").mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve
        }),
    )
    const view = render(<ConversationSchedules threadId="th_home" />)
    await act(async () => {
      release({ schedules: fake.rows, created_by: ["sch_mint"] })
    })
    await waitFor(() => expect(screen.getByTestId("conversation-schedules")).toHaveTextContent("Created this chat"))

    pending.mockImplementation(
      () =>
        new Promise((resolve) => {
          release = resolve
        }),
    )
    view.rerender(<ConversationSchedules threadId="th_other" />)
    expect(screen.queryByText("Created this chat")).not.toBeInTheDocument()
    await act(async () => {
      release({ schedules: [], created_by: [] })
    })
    pending.mockRestore()
  })

  it("stays hidden when this conversation has no waits", async () => {
    fake.rows = [row({ id: "sch_else", thread_id: "th_other", title: "Aimed elsewhere" })]
    useApp.setState({ schedules: fake.rows })
    render(<ConversationSchedules threadId="th_home" />)
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    expect(screen.queryByTestId("conversation-schedules")).not.toBeInTheDocument()
  })

  it("removes a bound wait as soon as it completes and hides the empty rail", async () => {
    fake.rows = [row({ id: "sch_bound", title: "Aimed here" })]
    useApp.setState({ schedules: fake.rows })
    render(<ConversationSchedules threadId="th_home" />)
    expect(await screen.findByTestId("conversation-schedules")).toHaveTextContent("Aimed here")

    await act(async () => {
      fake.rows = fake.rows.map((item) => ({ ...item, status: "done" }))
      useApp.setState({ schedules: fake.rows })
    })
    expect(screen.queryByTestId("conversation-schedules")).not.toBeInTheDocument()

    await act(async () => {
      fake.rows = fake.rows.map((item) => ({ ...item, status: "paused" }))
      useApp.setState({ schedules: fake.rows })
    })
    expect(screen.getByTestId("conversation-schedules")).toHaveTextContent("Paused")
    expect(screen.getByTestId("conversation-schedules")).not.toHaveTextContent("Next run now")
  })

  it("does not present a claimed one-shot as another wait while its turn is still running", async () => {
    // Claim consumes the one-shot before its run finishes. The rail describes
    // future wakes; Working remains the turn's independent execution state.
    fake.rows = [row({ id: "sch_claimed", status: "done", delay_s: 3600, every_s: 0 })]
    useApp.setState({ schedules: fake.rows, status: { running: true } })
    render(<ConversationSchedules threadId="th_home" />)
    await act(async () => undefined)
    expect(screen.queryByTestId("conversation-schedules")).not.toBeInTheDocument()
    expect(useApp.getState().status.running).toBe(true)
  })

  it.each([
    ["done", "Done"],
    ["cancelled", "Cancelled"],
  ])("retains the %s job that created this chat with its real status", async (status, label) => {
    fake.createdBy = ["sch_mint"]
    fake.rows = [
      row({
        id: "sch_mint",
        kind: "standalone",
        thread_id: "",
        title: "Created this chat",
        status,
      }),
    ]
    useApp.setState({ schedules: fake.rows })
    render(<ConversationSchedules threadId="th_home" />)
    const list = await screen.findByTestId("conversation-schedules")
    expect(list).toHaveTextContent("Created this chat")
    expect(list).toHaveTextContent(label)
    expect(list).not.toHaveTextContent("Next run now")
  })
})
