import { expect, it } from "vitest"
import { setLocale } from "./i18n"
import { applyEvent, phoneAgents } from "./transcript"

it("shows a peer notice with its source without making a human message or worker", () => {
  setLocale("zh")
  const blocks = applyEvent([], { thread_id: "receiver", seq: 1, kind: "task_message", agent_id: "manager",
    source_thread_id: "peer", source_turn_id: "source", text: "等待回复", created_at: "2026-01-01T00:00:00Z" })
  expect(blocks.map((b) => b.kind)).toEqual(["notice"])
  expect(blocks[0].text).toBe("协作任务 peer：等待回复")
  expect(phoneAgents(blocks)).toEqual([])
  expect(applyEvent(blocks, { thread_id: "receiver", seq: 1, kind: "task_message", agent_id: "manager", source_thread_id: "peer", text: "等待回复", created_at: "2026-01-01T00:00:00Z" })).toHaveLength(1)
  setLocale("en")
})
