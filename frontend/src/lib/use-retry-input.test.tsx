import { act, renderHook } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useSettingsSheet } from "@/store/settings-sheet"
import { emptyTranscript, reduceEvents, type TranscriptState } from "./transcript"
import type { Turn } from "./types"
import { useRetryInput } from "./use-retry-input"

const jumpTo = vi.fn()
const resend = vi.fn(async (_text: string, _seq: number) => true)
const send = vi.fn()
const loadUntilTurn = vi.fn()
const options = { enabled: true, jumpTo, resend }

function turn(id = "failed", patch: Partial<Turn> = {}): Turn {
  return { id, thread_id: "thread", seq: 1, status: "error", user_text: "Original request.", final: "", provider_id: "", model: "", started_at: "2026-01-01T00:00:00Z", duration_ms: 0, ...patch }
}

function failed(withInput = true, id = "failed"): TranscriptState {
  return reduceEvents(emptyTranscript(), [
    ...(withInput ? [{ thread_id: "thread", turn_id: id, agent_id: "manager", seq: 1, kind: "user_message" as const, text: "Original request.", created_at: "2026-01-01T00:00:00Z" }] : []),
    { thread_id: "thread", turn_id: id, agent_id: "manager", seq: 30, kind: "error", err: "The service refused the request.", created_at: "2026-01-01T00:00:01Z" },
  ])
}

beforeEach(() => {
  vi.clearAllMocks()
  resend.mockReset().mockResolvedValue(true)
  loadUntilTurn.mockReset().mockResolvedValue(false)
  useApp.setState({ activeId: "thread", completionVisit: 1, loaded: true, transcript: failed(), turns: [turn()], status: { running: false }, historyHasMore: false, meta: undefined, send, loadUntilTurn })
  useSettingsSheet.setState({ open: false })
})

it("immediately resends the recorded input from its actual sequence without synthetic text", async () => {
  const transcript = failed()
  transcript.agents.manager.blocks[0].images = [{ id: "original-image", name: "input.png", mime: "image/png" }]
  useApp.setState({ transcript })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).toHaveBeenCalledWith("Original request.", 1)
  expect(jumpTo).toHaveBeenCalledWith("failed")
  expect(jumpTo.mock.invocationCallOrder[0]).toBeLessThan(resend.mock.invocationCallOrder[0])
  expect(useApp.getState().transcript).toBe(transcript)
  expect(send).not.toHaveBeenCalled()
  expect(loadUntilTurn).not.toHaveBeenCalled()
})

it.each(["disabled", "loading", "running", "streaming", "asking", "restoring", "settings"])("does not resend the input while %s", async (mode) => {
  if (mode === "loading") useApp.setState({ loaded: false })
  if (mode === "running") useApp.setState({ status: { running: true } })
  if (mode === "streaming") useApp.setState({ transcript: { ...failed(), running: true } })
  if (mode === "asking") useApp.setState({ status: { running: false, awaiting_answer: true } })
  if (mode === "restoring") useApp.setState({ meta: { startup: "restoring" } as never })
  if (mode === "settings") useSettingsSheet.setState({ open: true })
  const { result } = renderHook(() => useRetryInput({ ...options, enabled: mode !== "disabled" }))
  // Temporary activity disables the visible action instead of removing it.
  expect(result.current.canRetryInput("failed")).toBe(mode !== "disabled")
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
  expect(jumpTo).not.toHaveBeenCalled()
  expect(loadUntilTurn).not.toHaveBeenCalled()
  expect(send).not.toHaveBeenCalled()
})

it.each(["done", "cancelled", "running"] as const)("does not retry a %s turn", async (status) => {
  useApp.setState({ transcript: { ...failed(), turns: [{ id: "failed", userText: "Original request.", status, agentIds: [] }] } })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
})

it("does not guess a missing input from another turn or from API prose", async () => {
  const transcript = failed(false)
  transcript.agents.manager.blocks.unshift({ id: "older", agentId: "manager", kind: "user", text: "Other request.", turnId: "older", seq: 1, at: "" })
  useApp.setState({ transcript })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
})

it("does not use an old error after a newer API turn exists", async () => {
  useApp.setState({ turns: [turn(), turn("newer", { seq: 2, status: "done" })] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
})

it.each([{ goal_continue: true }, { schedule_continue: true }, { schedule_run_id: "run" }])("does not turn an automatic check into a human input: %j", async (patch) => {
  useApp.setState({ turns: [turn("failed", patch)] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
})

it("waits for turn metadata even when its input block has already loaded", async () => {
  const transcript = failed()
  transcript.agents.manager.blocks[0].text = "A legacy continuation may look like a user input."
  useApp.setState({ transcript, turns: [] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
  expect(jumpTo).not.toHaveBeenCalled()
  expect(loadUntilTurn).not.toHaveBeenCalled()
  expect(send).not.toHaveBeenCalled()
})

it("loads an image-only turn and resends its actual recorded input", async () => {
  useApp.setState({ transcript: failed(false), historyHasMore: true, turns: [turn("failed", { user_text: "" })] })
  const transcript = failed()
  transcript.agents.manager.blocks[0].text = ""
  transcript.agents.manager.blocks[0].images = [{ id: "image-only", mime: "image/png" }]
  loadUntilTurn.mockImplementation(async () => { useApp.setState({ transcript }); return true })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(loadUntilTurn).toHaveBeenCalledWith("failed")
  expect(resend).toHaveBeenCalledWith("", 1)
  expect(useApp.getState().transcript).toBe(transcript)
  expect(send).not.toHaveBeenCalled()
})

it("refuses an API turn without a stored turn identity", async () => {
  useApp.setState({ transcript: failed(false), historyHasMore: true, turns: [turn("failed", { seq: 0 })] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(loadUntilTurn).not.toHaveBeenCalled()
  expect(resend).not.toHaveBeenCalled()
})

it("loads the real input once before resending it after repeated clicks", async () => {
  let finish!: (found: boolean) => void
  loadUntilTurn.mockImplementation(() => new Promise<boolean>((resolve) => { finish = resolve }))
  useApp.setState({ transcript: failed(false), historyHasMore: true })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  let pending!: Promise<void>
  act(() => { pending = result.current.retryInput("failed") })
  expect(result.current.retryingTurn).toBe("failed")
  await act(async () => { await result.current.retryInput("failed") })
  expect(loadUntilTurn).toHaveBeenCalledTimes(1)
  expect(loadUntilTurn).toHaveBeenCalledWith("failed")
  await act(async () => { useApp.setState({ transcript: failed() }); finish(true); await pending })
  expect(resend).toHaveBeenCalledTimes(1)
  expect(resend).toHaveBeenCalledWith("Original request.", 1)
  expect(jumpTo).toHaveBeenCalledWith("failed")
  expect(result.current.retryingTurn).toBeUndefined()
  expect(send).not.toHaveBeenCalled()
})

it.each(["not-found", "throws", "no-recorded-seq"])("does not invent an input when history loading %s", async (mode) => {
  useApp.setState({ transcript: failed(false), historyHasMore: true })
  loadUntilTurn.mockImplementation(async () => {
    if (mode === "throws") throw new Error("History unavailable")
    if (mode === "no-recorded-seq") {
      const transcript = failed()
      transcript.agents.manager.blocks[0].seq = 0
      useApp.setState({ transcript })
      return true
    }
    return false
  })
  const { result } = renderHook(() => useRetryInput(options))
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
  expect(jumpTo).not.toHaveBeenCalled()
  expect(result.current.retryingTurn).toBeUndefined()
})

it.each(["navigation", "revisit", "running", "streaming", "later-turn", "asking", "restoring", "settings", "disabled"])("refuses a late history result after %s", async (mode) => {
  let finish!: (found: boolean) => void
  loadUntilTurn.mockImplementation(() => new Promise<boolean>((resolve) => { finish = resolve }))
  useApp.setState({ transcript: failed(false), historyHasMore: true })
  const { result, rerender } = renderHook(({ enabled }) => useRetryInput({ ...options, enabled }), { initialProps: { enabled: true } })
  let pending!: Promise<void>
  act(() => { pending = result.current.retryInput("failed") })
  await act(async () => {
    useApp.setState({ transcript: failed() })
    if (mode === "navigation") useApp.setState({ activeId: "other" })
    if (mode === "revisit") useApp.setState({ completionVisit: 2 })
    if (mode === "running") useApp.setState({ status: { running: true } })
    if (mode === "streaming") useApp.setState({ transcript: { ...failed(), running: true } })
    if (mode === "later-turn") useApp.setState({ transcript: { ...failed(), turns: [...failed().turns, { id: "later", status: "done", userText: "Later input.", agentIds: [] }] } })
    if (mode === "asking") useApp.setState({ status: { running: false, awaiting_answer: true } })
    if (mode === "restoring") useApp.setState({ meta: { startup: "restoring" } as never })
    if (mode === "settings") useSettingsSheet.setState({ open: true })
  })
  if (mode === "disabled") rerender({ enabled: false })
  await act(async () => { finish(true); await pending })
  expect(resend).not.toHaveBeenCalled()
  expect(jumpTo).not.toHaveBeenCalled()
})

it("does not resend after the transcript unmounts while loading", async () => {
  let finish!: (found: boolean) => void
  loadUntilTurn.mockImplementation(() => new Promise<boolean>((resolve) => { finish = resolve }))
  useApp.setState({ transcript: failed(false), historyHasMore: true })
  const { result, unmount } = renderHook(() => useRetryInput(options))
  let pending!: Promise<void>
  act(() => { pending = result.current.retryInput("failed") })
  unmount()
  useApp.setState({ transcript: failed() })
  await act(async () => { finish(true); await pending })
  expect(resend).not.toHaveBeenCalled()
})

it("follows recorded retry ancestry back to the human input", async () => {
  const transcript = failed(true, "original")
  const retry = failed(true)
  transcript.agents.manager.blocks.push(...retry.agents.manager.blocks.map((b) => ({ ...b, seq: b.seq + 30 })))
  transcript.turns.push(...retry.turns)
  useApp.setState({ transcript, turns: [turn("original"), turn("middle", { seq: 2, retry_turn_id: "original" }), turn("failed", { seq: 3, retry_turn_id: "middle" })] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).toHaveBeenCalledWith("Original request.", 1)
  expect(jumpTo).toHaveBeenCalledWith("original")
})

it.each(["missing", "cycle", "cross-thread", "automatic", "forward"])("refuses a %s retry ancestry instead of resending a synthetic input", async (mode) => {
  const source = turn("original")
  const current = turn("failed", { seq: 2, retry_turn_id: "original" })
  if (mode === "missing") current.retry_turn_id = "absent"
  if (mode === "cycle") source.retry_turn_id = "failed"
  if (mode === "cross-thread") source.thread_id = "other"
  if (mode === "automatic") source.goal_continue = true
  if (mode === "forward") source.seq = 3
  useApp.setState({ turns: [source, current] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).not.toHaveBeenCalled()
})

it("does not classify human text as synthetic by matching its wording", async () => {
  const transcript = failed()
  transcript.agents.manager.blocks[0].text = "请从中断处继续当前任务。"
  useApp.setState({ transcript, turns: [turn("failed", { user_text: "请从中断处继续当前任务。" })] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).toHaveBeenCalledWith("请从中断处继续当前任务。", 1)
})

it.each([true, false])("keeps one retry pending until its submission acknowledges %s", async (accepted) => {
  let acknowledge!: (accepted: boolean) => void
  resend.mockImplementationOnce(() => new Promise<boolean>((resolve) => { acknowledge = resolve }))
  const { result } = renderHook(() => useRetryInput(options))
  let pending!: Promise<void>
  act(() => { pending = result.current.retryInput("failed") })
  expect(resend).toHaveBeenCalledWith("Original request.", 1)
  expect(result.current.retryingTurn).toBe("failed")
  await act(async () => { await result.current.retryInput("failed") })
  expect(resend).toHaveBeenCalledTimes(1)
  await act(async () => { acknowledge(accepted); await pending })
  expect(result.current.retryingTurn).toBeUndefined()
  if (!accepted) {
    await act(async () => { await result.current.retryInput("failed") })
    expect(resend).toHaveBeenCalledTimes(2)
  }
})
