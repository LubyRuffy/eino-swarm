import { act, render, screen } from "@testing-library/react"
import { expect, it } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"
import { Transcript } from "./transcript"

function event(kind: string, seq: number, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return { kind, seq, thread_id: "thread", turn_id: "turn", agent_id: "manager",
    created_at: `2026-01-01T00:00:${String(seq).padStart(2, "0")}.000Z`, ...extra }
}

// Retry is a past receipt, not a status spinner. The terminal refusal must
// remain a separate row after live delivery and a cold history replay.
it("shows a correction receipt and terminal refusal on separate rows through live and cold replay", () => {
  act(() => useApp.setState({ locale: "zh" }))
  const before = [
    event("user_message", 1, { text: "Inspect current state." }),
    event("goal_session", 2),
    event("model_retry", 3, { text: '{"reason":"schedule_claim_unverified","attempt":1,"cap":1,"code":"no_matching_schedule"}' }),
  ]
  const terminal = event("error", 4, {
    err: "schedule_claim_unverified: the claimed future check could not be verified; no unverified answer was published (no_matching_schedule)",
  })
  const waiting = reduceEvents(emptyTranscript(), before)
  const view = render(<Transcript state={waiting} loaded onSelectAgent={() => {}} />)
  const retry = "上一份终稿的调度声明无法核对，已请求一次纠正。"
  const failure = "本回合的调度声明无法核对，终稿未发布（schedule_claim_unverified / no_matching_schedule）。"
  expect(screen.getByText(retry)).toBeVisible()
  expect(screen.queryByText(failure)).not.toBeInTheDocument()
  const failed = reduceEvents(waiting, [terminal])
  for (const state of [failed, reduceEvents(emptyTranscript(), [...before, terminal])]) {
    view.rerender(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    const receiptRow = screen.getByText(retry).closest('[data-testid="memory-notice"]')!
    const errorLine = screen.getByText(failure)
    expect(receiptRow).toBeVisible()
    expect(errorLine).toBeVisible()
    expect(receiptRow).not.toContainElement(errorLine)
    expect(receiptRow.compareDocumentPosition(errorLine) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByText("正在核对调度记录，确认后再报告已安排。")).not.toBeInTheDocument()
    expect(screen.queryByText("目标已卡住：需要你或外部条件才能继续。")).not.toBeInTheDocument()
    expect(state.turns.at(-1)).toMatchObject({ status: "error", error: terminal.err })
  }
})
