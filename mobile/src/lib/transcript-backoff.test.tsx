import { render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"

import { renderBlock } from "@/components/thread-blocks"
import { setLocale } from "./i18n"
import type { RemoteEvent } from "./rpc"
import { applyEvent, phoneAgents } from "./transcript"

afterEach(() => setLocale("en"))

function event(text: object, seq: number, agent_id = "manager"): RemoteEvent {
  return { thread_id: "thread", turn_id: "turn", seq, kind: "model_backoff", created_at: "2026-01-01T00:00:00Z", agent_id, text: JSON.stringify(text) }
}

it("shows one service cooldown across manager and worker callers on live and history replay", () => {
  const events = [
    event({ scope: "origin", cooldown_id: "service-a:1", wait_ms: 4999 }, 1),
    event({ scope: "origin", cooldown_id: "service-a:1", wait_ms: 2000 }, 2, "worker-a"),
    event({ scope: "origin", cooldown_id: "service-a:1", wait_ms: 0 }, 3, "worker-b"),
    event({ scope: "origin", cooldown_id: "service-a:2", wait_ms: 10000 }, 4),
  ]
  const live = events.reduce(applyEvent, [])
  expect(live.map((block) => block.text)).toEqual([
    "Waiting 5s before retrying the model request.",
    "Waiting 10s before retrying the model request.",
  ])
  expect(phoneAgents(live)).toEqual([])
  expect(events.reduce(applyEvent, [])).toEqual(live)
  expect(applyEvent(live, events[0])).toEqual(live)
  setLocale("zh")
  render(renderBlock(live[0]))
  expect(screen.getByText("等待 5 秒后重试模型请求。")).toBeVisible()
})

it("keeps the active-probe unknown duration out of the phone chat", () => {
  expect(applyEvent([], event({ scope: "origin", cooldown_id: "service:1", wait_ms: 0 }, 1))).toEqual([])
})

it("describes private answer recovery separately and never adds a classifier worker", () => {
  const events = [
    event({ scope: "schedule_answer", candidate_id: "private-answer", code: "classification_unavailable", wait_ms: 5000 }, 1, "schedule-answer-policy"),
    event({ scope: "schedule_answer", candidate_id: "private-answer", code: "classification_deferred", wait_ms: 0 }, 2, "schedule-answer-policy"),
  ]
  const blocks = events.reduce(applyEvent, [])
  expect(blocks.map((block) => block.text)).toEqual([
    "Waiting 5s before checking the unpublished answer again.",
    "Answer verification is unavailable. The unpublished answer is retained.",
  ])
  expect(phoneAgents(blocks)).toEqual([])
  expect(JSON.stringify(blocks)).not.toContain("private-answer")
  setLocale("zh")
  render(<>{blocks.map((block) => <div key={block.id}>{renderBlock(block)}</div>)}</>)
  expect(screen.getByText("等待 5 秒后重试未发布答复的核对。")).toBeVisible()
  expect(screen.getByText("答复核对暂不可用，未发布候选已保留。")).toBeVisible()
})
