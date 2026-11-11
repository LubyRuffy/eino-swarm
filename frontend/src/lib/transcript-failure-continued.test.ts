import { describe, expect, it } from "vitest"

import { emptyTranscript, reduceEvents } from "./transcript"

describe("failure continuation", () => {
  it("hides that turn's error and keeps the tool progress already shown", () => {
    const before = reduceEvents(emptyTranscript(), [
      { thread_id: "thread", turn_id: "older", agent_id: "manager", seq: 1, kind: "error", err: "An older failure.", created_at: "2026-01-01T00:00:00Z" },
      { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 2, kind: "user_message", text: "Inspect the assigned work.", created_at: "2026-01-01T00:00:01Z" },
      { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 3, kind: "tool_call", tool_call_id: "call-1", text: "exec({\"command\":\"printf ok\"})", created_at: "2026-01-01T00:00:02Z" },
      { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 4, kind: "tool_result", tool_call_id: "call-1", text: "committed receipt", created_at: "2026-01-01T00:00:03Z" },
      { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 5, kind: "error", err: "The service refused the request.", created_at: "2026-01-01T00:00:04Z" },
    ])
    const next = reduceEvents(before, [
      { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq: 6, kind: "failure_continued", text: "Continuing from the failed step.", created_at: "2026-01-01T00:00:05Z" },
    ])
    const blocks = next.agents.manager.blocks
    expect(blocks.some((block) => block.kind === "error" && block.turnId === "turn")).toBe(false)
    expect(blocks.some((block) => block.kind === "error" && block.text === "An older failure.")).toBe(true)
    expect(blocks.some((block) => block.kind === "tool" && block.tool?.result === "committed receipt" && block.tool.pending !== true)).toBe(true)
    expect(blocks.some((block) => block.kind === "user" && block.text === "Inspect the assigned work.")).toBe(true)
    expect(blocks.filter((block) => block.kind === "notice" && block.text === "Continuing from the failed step.")).toHaveLength(1)
    expect(next.running).toBe(true)
    expect(next.turns.find((turn) => turn.id === "turn")?.status).toBe("running")
  })
})
