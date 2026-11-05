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

const invalidDetails = [
  ["missing_message", "核对响应缺少正文或包含工具调用。"],
  ["json", "核对响应不是符合要求的 JSON。"],
  ["trailing", "核对响应的 JSON 后还有多余内容。"],
  ["claim_list", "核对响应的 claims 列表无效。"],
  ["claim_meaning", "核对声明的立场、时间或归属无效。"],
  ["claim_shape", "核对声明的必需字段或枚举无效。"],
  ["unspecified_scope_quote", "未指定范围的声明却附带了范围引文。"],
  ["scope_quote", "声明的范围引文缺失或没有原文依据。"],
  ["schedule_id", "声明的计划 ID 不在引文中。"],
  ["quote_not_in_candidate", "声明引文不是候选答复中的原文。"],
  ["next_run_at", "声明的下次运行时间不是 RFC3339 格式。"],
] as const

it.each(invalidDetails)("explains invalid classifier detail %s without rendering its payload", (detail, reason) => {
  act(() => useApp.setState({ locale: "zh" }))
  const state = reduceEvents(emptyTranscript(), [wait(1, "schedule-answer-policy", {
    scope: "schedule_answer", candidate_id: "private-candidate", code: "classification_invalid", detail, attempt: 2,
    cause: "private-model-fragment", quote: "private-quote",
  })])
  render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
  fireEvent.click(screen.getByRole("button", { name: "重试说明" }))
  expect(screen.getByText(reason)).toBeVisible()
  expect(document.body).not.toHaveTextContent("private-")
})

it.each([
  ["unavailable", "连续核对请求失败达到上限，已暂缓。"],
  ["invalid", "连续核对响应格式无效达到上限，已暂缓。"],
  ["attempts", "这份候选答复的核对总尝试次数达到上限，已暂缓。"],
])("explains deferral limit %s without promising another attempt", (detail, reason) => {
  act(() => useApp.setState({ locale: "zh" }))
  const state = reduceEvents(emptyTranscript(), [wait(1, "schedule-answer-policy", {
    scope: "schedule_answer", code: "classification_deferred", detail, wait_ms: 0,
  })])
  render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
  expect(screen.getByText("答复核对暂不可用，未发布候选已保留。")).toBeVisible()
  fireEvent.click(screen.getByRole("button", { name: "重试说明" }))
  expect(screen.getByText(reason)).toBeVisible()
  expect(screen.queryByText("本次等待发生在未发布答复的调度声明核对步骤，核对恢复前保留原候选答复。")).not.toBeInTheDocument()
})

it.each([undefined, "private-model-fragment", "attempts", { error: "private-model-fragment" }])("falls back safely for an invalid detail %j", (detail) => {
  act(() => useApp.setState({ locale: "zh" }))
  const state = reduceEvents(emptyTranscript(), [wait(1, "schedule-answer-policy", { scope: "schedule_answer", code: "classification_invalid", detail })])
  render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
  fireEvent.click(screen.getByRole("button", { name: "重试说明" }))
  expect(screen.getByText("核对响应格式无效。")).toBeVisible()
  expect(JSON.stringify(state.agents)).not.toContain("private-model-fragment")
})

it("keeps an unscoped legacy invalid receipt's original explanation", () => {
  act(() => useApp.setState({ locale: "zh" }))
  const state = reduceEvent(emptyTranscript(), { ...wait(1), text: '{"code":"classification_invalid","wait_ms":5000}' })
  render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
  fireEvent.click(screen.getByRole("button", { name: "重试说明" }))
  expect(screen.getByText("核对响应格式无效。")).toBeVisible()
})
