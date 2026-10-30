import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import { emptyTranscript, reduceEvent, reduceEvents, rewindTranscript } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"
import { Transcript } from "./transcript"

function wait(seq: number, agent_id = "manager", payload: Record<string, unknown> = {}): SwarmEvent {
  return { thread_id: "thread", turn_id: "turn", seq, agent_id, kind: "model_backoff", created_at: "2026-01-01T00:00:00Z",
    text: JSON.stringify({ scope: "origin", cooldown_id: "service:1", attempt: 2, wait_ms: 1000, ...payload }) }
}
afterEach(() => act(() => useApp.setState({ locale: "en" })))

it.each(["zh", "en"] as const)("does not assign verification counts to an unscoped legacy wait in %s", (locale) => {
  act(() => useApp.setState({ locale }))
  const state = reduceEvent(emptyTranscript(), { ...wait(1), text: '{"attempt":2,"wait_ms":5000}' })
  render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
  fireEvent.click(screen.getByRole("button", { name: locale === "zh" ? "重试说明" : "Retry details" }))
  const details = screen.getByRole("region", { name: locale === "zh" ? "重试说明" : "Retry details" })
  expect(details).toHaveTextContent(locale === "zh" ? "未记录具体重试原因" : "Its exact retry cause was not recorded")
  expect(details).not.toHaveTextContent(/核对尝试次数|Verification attempt|服务连续失败次数|Consecutive service failures/)
})

it("expands actual callers in live and history views without inventing a failed step", () => {
  act(() => useApp.setState({ locale: "zh" }))
  const first = wait(1)
  const worker = { ...wait(2, "worker-42"), role: "researcher" }
  const probe = wait(3, "title-namer", { wait_ms: 0 })
  const initial = reduceEvent(emptyTranscript(), first)
  const live = reduceEvents(initial, [worker, probe])
  expect(initial.agents.manager.blocks[0].backoff?.callers).toHaveLength(1)
  expect(live.agents.manager.blocks).toHaveLength(1)
  expect(live.agentOrder).toEqual(["manager"])
  expect(reduceEvents(emptyTranscript(), [first, worker, probe])).toEqual(live)
  expect(reduceEvent(live, worker)).toEqual(live)
  const view = render(<Transcript state={live} loaded onSelectAgent={() => {}} />)
  const toggle = screen.getByRole("button", { name: "重试说明" })
  expect(toggle).toHaveAttribute("aria-expanded", "false")
  expect(screen.getByText("主管模型请求 · manager")).not.toBeVisible()
  fireEvent.click(toggle)
  expect(screen.getByRole("region", { name: "重试说明" })).toBeVisible()
  expect(screen.getByText("主管模型请求 · manager")).toBeVisible()
  expect(screen.getByText("模型请求 · researcher · worker-42")).toBeVisible()
  expect(screen.getByText("对话或调度标题生成 · title-namer")).toBeVisible()
  expect(screen.getByText(/不代表每个步骤都发生了失败/)).toBeVisible()
  expect(screen.getByText("服务连续失败次数：2")).toBeVisible()
  act(() => useApp.setState({ locale: "en" }))
  expect(screen.getByText("Manager model request · manager")).toBeVisible()
  fireEvent.click(screen.getByRole("button", { name: "Hide details" }))
  expect(screen.getByRole("button", { name: "Retry details" })).toHaveAttribute("aria-expanded", "false")
  view.rerender(<Transcript state={reduceEvents(emptyTranscript(), [first, worker, probe])} loaded onSelectAgent={() => {}} />)
  fireEvent.click(screen.getByRole("button", { name: "Retry details" }))
  expect(screen.getByText("Model request · researcher · worker-42")).toBeVisible()
  const rewound = rewindTranscript(live, 2)
  expect(rewound.agents.manager.blocks[0].backoff?.callers.map((caller) => caller.agentId)).toEqual(["manager"])
  expect(live.agents.manager.blocks[0].backoff?.callers).toHaveLength(3)
})

it("explains candidate verification and malformed legacy receipts without exposing private payloads", () => {
  act(() => useApp.setState({ locale: "zh" }))
  const state = reduceEvents(emptyTranscript(), [
    wait(1, "schedule-answer-policy", { scope: "schedule_answer", candidate_id: "private-candidate", code: "classification_invalid", attempt: 1 }),
    { ...wait(2, "memory-reviewer"), text: "bad private payload" },
  ])
  render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
  for (const button of screen.getAllByRole("button", { name: "重试说明" })) fireEvent.click(button)
  expect(screen.getByText("调度答复核对 · schedule-answer-policy")).toBeVisible()
  expect(screen.getByText("核对响应格式无效。")).toBeVisible()
  expect(screen.getByText("核对尝试次数：1")).toBeVisible()
  expect(screen.getByText("记忆复核 · memory-reviewer")).toBeVisible()
  expect(screen.getByText(/未记录具体重试原因/)).toBeVisible()
  expect(document.body).not.toHaveTextContent("private-candidate")
  expect(document.body).not.toHaveTextContent("bad private payload")
})
