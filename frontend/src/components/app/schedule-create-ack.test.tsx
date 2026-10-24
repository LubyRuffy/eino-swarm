import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { ScheduleDetail } from "./schedule-inbox"
import { useApp } from "@/store/app"
import { useScheduleInboxUI } from "@/store/schedule-inbox-ui"

const fake = vi.hoisted(() => ({ error: "", created: [] as unknown[], waiting: undefined as Promise<void> | undefined }))
vi.mock("@/lib/api", () => ({ ApiError: Error, api: {
  schedules: async () => ({ schedules: [], unread: 0 }),
  createSchedule: async (body: unknown) => {
    fake.created.push(body)
    if (fake.waiting) await fake.waiting
    if (fake.error) throw new Error(fake.error)
    return { id: "created" }
  },
} }))

beforeEach(() => {
  fake.error = ""; fake.created = []; fake.waiting = undefined
  useApp.setState({ locale: "en", schedules: [], scheduleInboxOpen: true, meta: undefined, error: undefined, activeId: undefined })
  useScheduleInboxUI.getState().reset()
  useScheduleInboxUI.getState().openCreate()
})
function prepare() {
  render(<ScheduleDetail />)
  fireEvent.change(screen.getByLabelText("Task"), { target: { value: "Keep this scheduled instruction" } })
  fireEvent.change(screen.getByLabelText("Delay (seconds)"), { target: { value: "60" } })
}
describe("schedule creation acknowledgements", () => {
  it.each(["restoring", "request failed"])("retains the original form and values after %s", async (reason) => {
    fake.error = reason
    prepare()
    fireEvent.click(screen.getByRole("button", { name: "Add wait" }))
    await waitFor(() => expect(useApp.getState().error).toBe(reason))
    expect(screen.getByTestId("schedule-create-drawer")).toBeVisible()
    expect(screen.getByLabelText("Task")).toHaveValue("Keep this scheduled instruction")
    expect(screen.getByLabelText("Delay (seconds)")).toHaveValue(60)
    expect(fake.created).toHaveLength(1)
  })
  it("keeps the form pending and excludes duplicate submissions until a successful acknowledgement", async () => {
    let finish!: () => void
    fake.waiting = new Promise<void>((done) => { finish = done })
    prepare()
    const submit = screen.getByRole("button", { name: "Add wait" })
    fireEvent.click(submit)
    expect(submit).toBeDisabled()
    expect(screen.getByTestId("schedule-create-drawer")).toBeVisible()
    fireEvent.click(submit)
    expect(fake.created).toHaveLength(1)
    await act(async () => { finish() })
    await waitFor(() => expect(screen.queryByTestId("schedule-create-drawer")).toBeNull())
  })
  it("does not close a different create form after an earlier request is acknowledged", async () => {
    let finish!: () => void
    fake.waiting = new Promise<void>((done) => { finish = done })
    prepare()
    fireEvent.click(screen.getByRole("button", { name: "Add wait" }))
    act(() => { useScheduleInboxUI.getState().closeDrawer(); useScheduleInboxUI.getState().openCreate() })
    await act(async () => { finish() })
    expect(screen.getByTestId("schedule-create-drawer")).toBeVisible()
  })
})
