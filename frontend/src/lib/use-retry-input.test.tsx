import { act, renderHook } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useSettingsSheet } from "@/store/settings-sheet"
import { emptyTranscript, reduceEvents, type TranscriptState } from "./transcript"
import type { Turn } from "./types"
import { useRetryInput } from "./use-retry-input"

const continueFailed = vi.fn(async (_turnId: string) => true)
const send = vi.fn()
const loadUntilTurn = vi.fn()
const options = { enabled: true, continueFailed }

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
  continueFailed.mockReset().mockResolvedValue(true)
  loadUntilTurn.mockReset().mockResolvedValue(false)
  useApp.setState({ activeId: "thread", completionVisit: 1, loaded: true, transcript: failed(), turns: [turn()], status: { running: false }, historyHasMore: false, meta: undefined, send, loadUntilTurn })
  useSettingsSheet.setState({ open: false })
})

it("continues the failed turn without resending or rewinding its input", async () => {
  const transcript = failed()
  transcript.agents.manager.blocks[0].images = [{ id: "original-image", name: "input.png", mime: "image/png" }]
  useApp.setState({ transcript })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(continueFailed).toHaveBeenCalledTimes(1)
  expect(useApp.getState().transcript).toBe(transcript)
  expect(send).not.toHaveBeenCalled()
  expect(loadUntilTurn).not.toHaveBeenCalled()
})

it.each(["disabled", "loading", "running", "streaming", "asking", "restoring", "settings"])("does not continue the failure while %s", async (mode) => {
  if (mode === "loading") useApp.setState({ loaded: false })
  if (mode === "running") useApp.setState({ status: { running: true } })
  if (mode === "streaming") useApp.setState({ transcript: { ...failed(), running: true } })
  if (mode === "asking") useApp.setState({ status: { running: false, awaiting_answer: true } })
  if (mode === "restoring") useApp.setState({ meta: { startup: "restoring" } as never })
  if (mode === "settings") useSettingsSheet.setState({ open: true })
  const { result } = renderHook(() => useRetryInput({ ...options, enabled: mode !== "disabled" }))
  expect(result.current.canRetryInput("failed")).toBe(mode !== "disabled")
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).not.toHaveBeenCalled()
  expect(loadUntilTurn).not.toHaveBeenCalled()
  expect(send).not.toHaveBeenCalled()
})

it.each(["done", "cancelled", "running"] as const)("does not retry a %s turn", async (status) => {
  useApp.setState({ transcript: { ...failed(), turns: [{ id: "failed", userText: "Original request.", status, agentIds: [] }] } })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).not.toHaveBeenCalled()
})

it("does not continue a failure that has no stored user block", async () => {
  const transcript = failed(false)
  transcript.agents.manager.blocks.unshift({ id: "older", agentId: "manager", kind: "user", text: "Other request.", turnId: "older", seq: 1, at: "" })
  useApp.setState({ transcript })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).not.toHaveBeenCalled()
})

it("does not use an old error after a newer API turn exists", async () => {
  useApp.setState({ turns: [turn(), turn("newer", { seq: 2, status: "done" })] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).not.toHaveBeenCalled()
})

it.each([{ goal_continue: true }, { schedule_continue: true }, { schedule_run_id: "run" }])("does not continue an automatic check: %j", async (patch) => {
  useApp.setState({ turns: [turn("failed", patch)] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).not.toHaveBeenCalled()
})

it("waits for turn metadata even when its input block has already loaded", async () => {
  const transcript = failed()
  transcript.agents.manager.blocks[0].text = "A legacy continuation may look like a user input."
  useApp.setState({ transcript, turns: [] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).not.toHaveBeenCalled()
  expect(loadUntilTurn).not.toHaveBeenCalled()
  expect(send).not.toHaveBeenCalled()
})

it("loads an image-only turn and continues that same turn", async () => {
  useApp.setState({ transcript: failed(false), historyHasMore: true, turns: [turn("failed", { user_text: "" })] })
  const transcript = failed()
  transcript.agents.manager.blocks[0].text = ""
  transcript.agents.manager.blocks[0].images = [{ id: "image-only", mime: "image/png" }]
  loadUntilTurn.mockImplementation(async () => { useApp.setState({ transcript }); return true })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(loadUntilTurn).toHaveBeenCalledWith("failed")
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(useApp.getState().transcript).toBe(transcript)
  expect(send).not.toHaveBeenCalled()
})

it("refuses an API turn without a stored turn identity", async () => {
  useApp.setState({ transcript: failed(false), historyHasMore: true, turns: [turn("failed", { seq: 0 })] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(loadUntilTurn).not.toHaveBeenCalled()
  expect(continueFailed).not.toHaveBeenCalled()
})

it("loads the real input once before continuing after repeated clicks", async () => {
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
  expect(continueFailed).toHaveBeenCalledTimes(1)
  expect(continueFailed).toHaveBeenCalledWith("failed")
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
  expect(continueFailed).not.toHaveBeenCalled()
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
  expect(continueFailed).not.toHaveBeenCalled()
})

it("does not continue after the transcript unmounts while loading", async () => {
  let finish!: (found: boolean) => void
  loadUntilTurn.mockImplementation(() => new Promise<boolean>((resolve) => { finish = resolve }))
  useApp.setState({ transcript: failed(false), historyHasMore: true })
  const { result, unmount } = renderHook(() => useRetryInput(options))
  let pending!: Promise<void>
  act(() => { pending = result.current.retryInput("failed") })
  unmount()
  useApp.setState({ transcript: failed() })
  await act(async () => { finish(true); await pending })
  expect(continueFailed).not.toHaveBeenCalled()
})

it("continues the latest failed turn and does not walk retry_turn_id", async () => {
  const transcript = failed(true, "original")
  const retry = failed(true)
  transcript.agents.manager.blocks.push(...retry.agents.manager.blocks.map((block) => ({ ...block, seq: block.seq + 30 })))
  transcript.turns.push(...retry.turns)
  useApp.setState({
    transcript,
    turns: [turn("original"), turn("middle", { seq: 2, retry_turn_id: "original" }), turn("failed", { seq: 3, retry_turn_id: "middle" })],
  })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  expect(result.current.canRetryInput("original")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(send).not.toHaveBeenCalled()
})

it.each(["missing", "cycle", "cross-thread", "automatic"])("keeps retry on the latest failed turn when stored lineage is %s", async (mode) => {
  const source = turn("original")
  const current = turn("failed", { seq: 2, retry_turn_id: "original" })
  if (mode === "missing") current.retry_turn_id = "absent"
  if (mode === "cycle") source.retry_turn_id = "failed"
  if (mode === "cross-thread") source.thread_id = "other"
  if (mode === "automatic") source.goal_continue = true
  useApp.setState({ turns: [source, current] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).toHaveBeenCalledWith("failed")
})

it("does not continue an older failure when a later turn is stored", async () => {
  const source = turn("original", { seq: 3 })
  const current = turn("failed", { seq: 2, retry_turn_id: "original" })
  useApp.setState({ turns: [current, source] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(false)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).not.toHaveBeenCalled()
})

it("does not classify human text as a reason to rewind", async () => {
  const transcript = failed()
  transcript.agents.manager.blocks[0].text = "请从中断处继续当前任务。"
  useApp.setState({ transcript, turns: [turn("failed", { user_text: "请从中断处继续当前任务。" })] })
  const { result } = renderHook(() => useRetryInput(options))
  expect(result.current.canRetryInput("failed")).toBe(true)
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(send).not.toHaveBeenCalled()
})

it.each([true, false])("keeps one retry pending until its submission acknowledges %s", async (accepted) => {
  let acknowledge!: (accepted: boolean) => void
  continueFailed.mockImplementationOnce(() => new Promise<boolean>((resolve) => { acknowledge = resolve }))
  const { result } = renderHook(() => useRetryInput(options))
  let pending!: Promise<void>
  act(() => { pending = result.current.retryInput("failed") })
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(result.current.retryingTurn).toBe("failed")
  await act(async () => { await result.current.retryInput("failed") })
  expect(continueFailed).toHaveBeenCalledTimes(1)
  await act(async () => { acknowledge(accepted); await pending })
  expect(result.current.retryingTurn).toBeUndefined()
  if (!accepted) {
    await act(async () => { await result.current.retryInput("failed") })
    expect(continueFailed).toHaveBeenCalledTimes(2)
  }
})
