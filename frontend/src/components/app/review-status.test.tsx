import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { ReviewOutcome, SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"
import { useTraceFocus } from "@/store/trace-focus"
import { ReviewStatus } from "./review-status"
import { RightPanel } from "./panel"

const outcome: ReviewOutcome = {
  changed: false, err: "token=secret", stop_reason: "iteration_limit", attempt_count: 2,
  refused: [{ index: -1, reason: "private original note", code: "capacity", usage: "240/240", over_by: 18 }],
  attempts: [{ number: 2, status: "refused", operations: [{ target: "memory", action: "add", sources: [7] }] }],
}

beforeEach(() => {
  useApp.setState({ locale: "en", activeId: undefined })
  useTraceFocus.setState({ turnId: undefined, request: 0 })
})

describe("review status", () => {
  it("shows controlled diagnostics in both languages and scopes the Trace to the original turn", () => {
    render(<ReviewStatus outcome={outcome} turnId="tn_original" panel />)
    expect(screen.getByTestId("review-status")).toHaveTextContent("240/240")
    expect(screen.getByTestId("review-status")).not.toHaveTextContent(/secret|private original/)
    fireEvent.click(screen.getByRole("button", { name: "Trace tn_original" }))
    expect(useTraceFocus.getState().turnId).toBe("tn_original")
    act(() => useApp.setState({ locale: "zh" }))
    expect(screen.getByTestId("review-status")).toHaveTextContent("超出 18 字符")
    expect(screen.getByTestId("review-status")).toHaveTextContent("本次笔记未保存")
  })

  it("loads the source turn before opening Trace and disables duplicate clicks", async () => {
    let finish!: (found: boolean) => void
    const load = vi.fn(() => new Promise<boolean>((resolve) => { finish = resolve }))
    useApp.setState({ activeId: "th_a", loadUntilTurn: load })
    render(<ReviewStatus outcome={outcome} turnId="tn_old" />)
    const button = screen.getByRole("button", { name: "Trace tn_old" })
    fireEvent.click(button)
    expect(button).toBeDisabled()
    expect(load).toHaveBeenCalledWith("tn_old")
    expect(useTraceFocus.getState().turnId).toBeUndefined()
    await act(async () => { finish(true) })
    expect(useTraceFocus.getState().turnId).toBe("tn_old")
    expect(button).toBeEnabled()
  })

  it("keeps a failed source read explicit and does not focus a different conversation after navigation", async () => {
    useApp.setState({ activeId: "th_a", loadUntilTurn: vi.fn().mockResolvedValue(false) })
    render(<ReviewStatus outcome={outcome} turnId="tn_old" />)
    const button = screen.getByRole("button", { name: "Trace tn_old" })
    fireEvent.click(button)
    await waitFor(() => expect(screen.getByRole("alert")).toBeVisible())
    expect(useTraceFocus.getState().turnId).toBeUndefined()
    let finish!: (found: boolean) => void
    useApp.setState({ loadUntilTurn: () => new Promise<boolean>((resolve) => { finish = resolve }) })
    fireEvent.click(button)
    act(() => useApp.setState({ activeId: "th_b" }))
    await act(async () => { finish(true) })
    expect(useTraceFocus.getState().turnId).toBeUndefined()
    useApp.setState({ activeId: "th_a", loadUntilTurn: vi.fn().mockRejectedValue(new Error("private source")) })
    fireEvent.click(button)
    await waitFor(() => expect(screen.getByRole("alert")).toBeVisible())
    expect(screen.getByRole("alert")).not.toHaveTextContent("private source")
  })

  it("opens an older turn's Full log while newer work remains the latest turn", () => {
    let seq = 0
    const event = (turn_id: string, kind: string, text: string): SwarmEvent => ({ thread_id: "th_a", turn_id, kind, text, seq: ++seq, agent_id: "manager", created_at: `2026-10-20T00:00:0${seq}Z` })
    const transcript = reduceEvents(emptyTranscript(), [
      event("tn_old", "user_message", "Initial request"), event("tn_old", "done", "Initial answer"),
      event("tn_new", "user_message", "New request"), event("tn_new", "done", "New answer"),
      event("tn_old", "memory_review", JSON.stringify(outcome)),
    ])
    const changeTab = vi.fn()
    const props = { tab: "memory" as const, onTabChange: changeTab, transcript, files: [], workspace: "", turns: [], onSelectAgent: vi.fn(), onUpload: async () => {}, onDeleteFile: vi.fn(), onRefreshFiles: vi.fn(), onReveal: vi.fn(), threadId: "th_a" }
    const view = render(<RightPanel {...props} />)
    act(() => useTraceFocus.getState().open("tn_old"))
    expect(changeTab).toHaveBeenCalledWith("trace")
    view.rerender(<RightPanel {...props} tab="trace" />)
    expect(screen.getByTestId("trace-log")).toHaveTextContent("2 attempts")
    expect(screen.getByTestId("trace-log")).not.toHaveTextContent("New request")
    fireEvent.click(screen.getByTestId("review-trace-details"))
    expect(screen.getByTestId("trace-log")).toHaveTextContent('"sources": [ 7 ]')
    expect(screen.getByTestId("trace-log")).not.toHaveTextContent(/secret|private original/)
    view.rerender(<RightPanel {...props} threadId="th_b" tab="trace" />)
    expect(useTraceFocus.getState().turnId).toBeUndefined()
  })
})
