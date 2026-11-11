import { act, fireEvent, render, screen } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import { OUTPUT_BUDGET_ERROR } from "@/lib/output-budget"
import { formatQuotedMessage } from "@/lib/quote"
import { localizeNotice } from "@/lib/i18n"
import type { Turn } from "@/lib/types"
import { useApp } from "@/store/app"
import { useSettingsSheet } from "@/store/settings-sheet"
import { Transcript } from "./transcript"

const send = vi.fn()
const failure = "the service rejected the request"
const original = "Inspect the current work."
const source: Turn = { id: "failed", thread_id: "thread", seq: 1, status: "error", user_text: original, final: "", provider_id: "", model: "", started_at: "2026-01-01T00:00:00Z", duration_ms: 1000 }

function failed(text = failure, input = original) {
  return reduceEvents(emptyTranscript(), [
    { thread_id: "thread", turn_id: "failed", agent_id: "manager", seq: 1, kind: "user_message", text: input, created_at: "2026-01-01T00:00:00Z" },
    { thread_id: "thread", turn_id: "failed", agent_id: "manager", seq: 2, kind: "error", err: text, created_at: "2026-01-01T00:00:01Z" },
  ])
}

beforeEach(() => {
  send.mockReset().mockResolvedValue(false)
  useApp.setState({ activeId: "thread", loaded: true, locale: "en", turns: [source], status: { running: false }, transcript: failed(), meta: undefined, historyHasMore: false, historyLoading: false, send })
  useSettingsSheet.setState({ open: false })
})

function show(onContinueFailed = vi.fn().mockResolvedValue(false)) {
  const onResendUser = vi.fn().mockResolvedValue(false)
  const view = render(<Transcript state={useApp.getState().transcript} loaded onSelectAgent={() => {}} onResendUser={onResendUser} onContinueFailed={onContinueFailed} />)
  return { ...view, onResendUser }
}

it.each([failure, OUTPUT_BUDGET_ERROR, "the model sent no first byte within 30s", "validation refused the final report"])("Retry continues the failed turn after: %s", async (text) => {
  const state = failed(text)
  useApp.setState({ transcript: state })
  const continueFailed = vi.fn().mockResolvedValue(true)
  const { onResendUser } = show(continueFailed)
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Retry" })) })
  expect(continueFailed).toHaveBeenCalledTimes(1)
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(onResendUser).not.toHaveBeenCalled()
  expect(screen.queryByTestId("user-message-editor")).not.toBeInTheDocument()
  expect(send).not.toHaveBeenCalled()
  expect(useApp.getState().transcript).toBe(state)
  expect(screen.getByText(localizeNotice(text, "en"), { selector: "p.stream-text" })).toBeInTheDocument()
})

it("submits once until the continuation finishes and allows another click after rejection", async () => {
  let release!: (accepted: boolean) => void
  const continueFailed = vi.fn().mockImplementationOnce(() => new Promise<boolean>((resolve) => { release = resolve })).mockResolvedValue(false)
  show(continueFailed)
  const retry = screen.getByRole("button", { name: "Retry" })
  fireEvent.click(retry)
  expect(retry).toBeDisabled()
  fireEvent.click(retry)
  expect(continueFailed).toHaveBeenCalledTimes(1)
  expect(screen.queryByTestId("user-message-editor")).not.toBeInTheDocument()
  await act(async () => { release(false) })
  expect(retry).toBeEnabled()
  await act(async () => { fireEvent.click(retry) })
  expect(continueFailed).toHaveBeenCalledTimes(2)
  expect(continueFailed).toHaveBeenNthCalledWith(2, "failed")
})

it("keeps the original quote and images in place while continuing the same turn", async () => {
  const input = formatQuotedMessage(["Quoted source."], original)
  const state = failed(failure, input)
  state.agents.manager.blocks[0].images = [{ id: "image", name: "selected.png", mime: "image/png" }]
  useApp.setState({ transcript: state })
  const continueFailed = vi.fn().mockResolvedValue(true)
  const { onResendUser } = show(continueFailed)
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Retry" })) })
  expect(continueFailed).toHaveBeenCalledTimes(1)
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(onResendUser).not.toHaveBeenCalled()
  expect(state.agents.manager.blocks[0].images).toEqual([{ id: "image", name: "selected.png", mime: "image/png" }])
  expect(screen.queryByTestId("user-message-editor")).not.toBeInTheDocument()
})

it.each(["running", "streaming", "restoring", "settings", "loading", "asking"])("disables Retry while %s", (mode) => {
  if (mode === "running") useApp.setState({ status: { running: true } })
  if (mode === "streaming") useApp.setState({ transcript: { ...failed(), running: true } })
  if (mode === "restoring") useApp.setState({ meta: { startup: "restoring" } as never })
  if (mode === "settings") useSettingsSheet.setState({ open: true })
  if (mode === "loading") useApp.setState({ loaded: false })
  if (mode === "asking") useApp.setState({ status: { running: false, awaiting_answer: true } })
  const continueFailed = vi.fn().mockResolvedValue(false)
  show(continueFailed)
  expect(screen.getByRole("button", { name: "Retry" })).toBeDisabled()
  fireEvent.click(screen.getByRole("button", { name: "Retry" }))
  expect(continueFailed).not.toHaveBeenCalled()
  expect(send).not.toHaveBeenCalled()
})

it.each(["done", "cancelled"] as const)("does not offer failure Retry on a %s turn", (status) => {
  const state = failed()
  state.turns[0].status = status
  useApp.setState({ transcript: state })
  show()
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument()
})

it("never borrows another turn's input when a failed automatic turn has none", () => {
  const state = reduceEvents(failed(), [{ thread_id: "thread", turn_id: "automatic", agent_id: "manager", seq: 3, kind: "error", err: "Automatic check failed.", created_at: "2026-01-01T00:00:02Z" }])
  useApp.setState({ transcript: state })
  const continueFailed = vi.fn().mockResolvedValue(false)
  show(continueFailed)
  expect(screen.queryByRole("button", { name: "Retry" })).not.toBeInTheDocument()
  expect(continueFailed).not.toHaveBeenCalled()
})

it("localizes Retry without editing the original submitted input", async () => {
  useApp.setState({ locale: "zh" })
  const continueFailed = vi.fn().mockResolvedValue(true)
  const { onResendUser } = show(continueFailed)
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "重试" })) })
  expect(continueFailed).toHaveBeenCalledTimes(1)
  expect(continueFailed).toHaveBeenCalledWith("failed")
  expect(onResendUser).not.toHaveBeenCalled()
  expect(screen.queryByTestId("user-message-editor")).not.toBeInTheDocument()
})
