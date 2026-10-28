import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { ReviewOutcome, SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"
import { useTraceFocus } from "@/store/trace-focus"
import { ReviewStatus } from "./review-status"
import { RightPanel } from "./panel"
import { CompactNotice } from "./compact-notice"

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
  it("opens committed additions, removals and replacements on demand and can close them", () => {
    render(<ReviewStatus outcome={{ changed: true, notes: { add: 1, replace: 1, remove: 1 }, changes: [
      { target: "memory", action: "replace", before: "Keep this condition\nPrevious preference", after: "Keep this condition\nUpdated preference" },
      { target: "memory", action: "add", before: "", after: "New durable preference" },
      { target: "memory", action: "remove", before: "Removed preference", after: "" },
    ] }} />)
    const button = screen.getByRole("button", { name: "View details" })
    expect(button).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByText("Previous preference")).not.toBeInTheDocument()
    fireEvent.click(button)
    const details = screen.getByRole("region", { name: "Memory changes" })
    expect(button).toHaveAttribute("aria-expanded", "true")
    expect(button).toHaveAttribute("aria-controls", details.id)
    const changes = within(details).getAllByTestId("memory-change")
    expect(changes.map((change) => within(change).getByText(/Note ·/).textContent)).toEqual(["Note · Updated", "Note · Added", "Note · Deleted"])
    expect(changes[0].querySelector('[data-diff="eq"]')).toHaveTextContent("Keep this condition")
    expect(changes[0].querySelector('[data-diff="del"]')).toHaveTextContent("Previous preference")
    expect(changes[0].querySelector('[data-diff="add"]')).toHaveTextContent("Updated preference")
    expect(changes[1].querySelector('[data-diff="add"]')).toHaveTextContent("New durable preference")
    expect(changes[1].querySelector('[data-diff="del"]')).toBeNull()
    expect(changes[2].querySelector('[data-diff="del"]')).toHaveTextContent("Removed preference")
    expect(changes[2].querySelector('[data-diff="add"]')).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Hide details" }))
    expect(screen.queryByRole("region", { name: "Memory changes" })).not.toBeInTheDocument()
  })

  it("keeps full committed skill text and still exposes a partial review's failure Trace", () => {
    const fullText = `Complete saved procedure ${"retained step ".repeat(30)}`
    render(<ReviewStatus outcome={{ ...outcome, changed: true, changes: [
      { target: "skill_manage", action: "patch", name: "procedure", text: "clipped preview", before: "Previous procedure", after: fullText },
    ] }} turnId="tn_partial" panel />)
    fireEvent.click(screen.getByRole("button", { name: "View details" }))
    expect(screen.getByRole("region", { name: "Memory changes" }).querySelector('[data-diff="add"] td:last-child')?.textContent).toBe(fullText)
    expect(screen.getByText("Skill procedure · Updated")).toBeVisible()
    expect(screen.getByRole("button", { name: "Trace tn_partial" })).toBeVisible()
    expect(screen.queryByText("clipped preview")).not.toBeInTheDocument()
    expect(screen.getByTestId("review-status")).not.toHaveTextContent(/token=secret|private original note/)
    act(() => useApp.setState({ locale: "zh" }))
    expect(screen.getByRole("button", { name: "收起详情" })).toBeVisible()
    expect(screen.getByRole("region", { name: "记忆修改详情" })).toHaveTextContent("技能 procedure · 修改")
  })

  it("shows an overwritten skill as updated even when its write action was create", () => {
    render(<ReviewStatus outcome={{ changed: true, changes: [
      { target: "skill_manage", action: "create", name: "existing-procedure", before: "Previous saved procedure", after: "Replacement saved procedure" },
      { target: "skill_manage", action: "create", name: "new-procedure", before: "", after: "New saved procedure" },
      { target: "skill_manage", action: "create", name: "legacy-procedure", text: "Saved preview" },
    ] }} />)
    fireEvent.click(screen.getByRole("button", { name: "View details" }))
    const changes = within(screen.getByRole("region", { name: "Memory changes" })).getAllByTestId("memory-change")
    expect(changes[0]).toHaveTextContent("Skill existing-procedure · Updated")
    expect(changes[0]).not.toHaveTextContent("· Added")
    expect(changes[0].querySelector('[data-diff="del"]')).toHaveTextContent("Previous saved procedure")
    expect(changes[0].querySelector('[data-diff="add"]')).toHaveTextContent("Replacement saved procedure")
    expect(changes[1]).toHaveTextContent("Skill new-procedure · Added")
    expect(changes[1].querySelector('[data-diff="del"]')).toBeNull()
    expect(changes[2]).toHaveTextContent("Skill legacy-procedure · Added")
  })

  it("identifies old previews without inventing removed or added text", () => {
    render(<ReviewStatus outcome={{ changed: true, changes: [
      { target: "memory", action: "replace", text: "Historical clipped preview", after: "Only one image exists" },
      { target: "skill_manage", action: "delete", name: "old-skill" },
    ] }} />)
    fireEvent.click(screen.getByRole("button", { name: "View details" }))
    const details = screen.getByRole("region", { name: "Memory changes" })
    expect(within(details).getAllByText(/Only a preview is available/)).toHaveLength(2)
    expect(details).toHaveTextContent("Historical clipped preview")
    expect(details).toHaveTextContent("Skill old-skill · Deleted")
    expect(details).not.toHaveTextContent("Only one image exists")
    expect(within(details).queryByTestId("file-diff")).not.toBeInTheDocument()
  })

  it("keeps the complete before and after text reachable when a long diff is clipped", () => {
    const prefix = Array.from({ length: 410 }, (_, index) => `Step ${index}`).join("\n")
    render(<ReviewStatus outcome={{ changed: true, changes: [
      { target: "skill_manage", action: "patch", name: "large-procedure", before: `${prefix}\nFinal old step`, after: `${prefix}\nFinal new step` },
    ] }} />)
    fireEvent.click(screen.getByRole("button", { name: "View details" }))
    expect(screen.queryByText("Final old step")).not.toBeInTheDocument()
    const button = screen.getByRole("button", { name: "View complete text" })
    expect(button).toHaveAttribute("aria-expanded", "false")
    fireEvent.click(button)
    const complete = screen.getByRole("region", { name: "View complete text" })
    expect(complete).toHaveTextContent("Before")
    expect(complete).toHaveTextContent("Final old step")
    expect(complete).toHaveTextContent("After")
    expect(complete).toHaveTextContent("Final new step")
    expect(button).toHaveAttribute("aria-controls", complete.id)
    fireEvent.click(screen.getByRole("button", { name: "Hide complete text" }))
    expect(screen.queryByRole("region", { name: "View complete text" })).not.toBeInTheDocument()
  })

  it("explains missing legacy details and leaves a no-change review quiet", () => {
    const view = render(<ReviewStatus outcome={{ changed: true, notes: { replace: 2 } }} />)
    fireEvent.click(screen.getByRole("button", { name: "View details" }))
    expect(screen.getByRole("region", { name: "Memory changes" })).toHaveTextContent("Change details were not recorded")
    view.rerender(<ReviewStatus outcome={{ changed: false }} />)
    expect(screen.queryByTestId("memory-notice")).not.toBeInTheDocument()
  })

  it("retains changes through event replay while an off notification stays hidden", () => {
    const saved = { changed: true, notify: "off", notes: { replace: 1 }, changes: [
      { target: "memory", action: "replace", before: "Original saved text", after: "Updated saved text" },
    ] }
    const event: SwarmEvent = { thread_id: "th_a", turn_id: "tn_review", seq: 1, kind: "memory_review", text: JSON.stringify(saved), agent_id: "memory-reviewer", created_at: "2026-10-20T00:00:00Z" }
    const block = reduceEvents(emptyTranscript(), [event]).agents.manager.blocks[0]
    const view = render(<CompactNotice block={block} />)
    expect(screen.queryByTestId("memory-notice")).not.toBeInTheDocument()
    view.rerender(<CompactNotice block={{ ...block, quiet: false }} />)
    fireEvent.click(screen.getByRole("button", { name: "View details" }))
    expect(screen.getByRole("region", { name: "Memory changes" })).toHaveTextContent("Original saved text")
    expect(screen.getByRole("region", { name: "Memory changes" })).toHaveTextContent("Updated saved text")
  })

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
