import { afterEach, expect, it } from "vitest"
import { setLocale } from "./i18n"
import { applyEvent } from "./transcript"

afterEach(() => setLocale("en"))

it("keeps a declined collection as a human stop notice", () => {
  const blocks = applyEvent([], { thread_id: "thread", turn_id: "turn", seq: 3, kind: "error", created_at: "2026-01-01T00:00:00Z", text: "", err: "stopped before collecting worker results" })
  expect(blocks[0].kind).toBe("notice")
  expect(blocks.some((block) => block.kind === "error")).toBe(false)
})

it("shows the worker wait reason without claiming tool rounds were exhausted", () => {
  setLocale("en")
  const blocks = applyEvent([], { thread_id: "thread", turn_id: "turn", seq: 1, kind: "max_iterations", created_at: "2026-01-01T00:00:00Z",
    text: JSON.stringify({ reason: "workers_uncollected", automatic_resume: true, limit: 80, extend_by: 2000 }) })
  expect(blocks).toHaveLength(1)
  expect(blocks[0].text).toMatch(/resume once/i)
  expect(blocks[0].text).not.toMatch(/80|2000|tool rounds/)
})

it("records automatic resumption as collection still to do", () => {
  setLocale("zh")
  const blocks = applyEvent([], { thread_id: "thread", turn_id: "turn", seq: 2, kind: "workers_collection_resumed", created_at: "2026-01-01T00:00:00Z", text: '{"worker_ids":["worker-1"]}' })
  expect(blocks).toHaveLength(1)
  expect(blocks[0].text).toContain("收取")
  expect(blocks[0].text).not.toContain("worker_ids")
})
