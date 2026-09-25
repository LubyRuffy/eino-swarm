import { describe, expect, it } from "vitest"

import { setLocale } from "./i18n"
import {
  applyEvent,
  foldPhoneItems,
  formatPhoneTicker,
  pendingAsk,
  phonePlanningTail,
  phoneWorkTicker,
  phoneAgents,
  managerBlocks,
  type CompactBlock,
} from "./transcript"
import type { RemoteEvent } from "./rpc"

function ev(partial: Partial<RemoteEvent> & Pick<RemoteEvent, "kind" | "seq">): RemoteEvent {
  return {
    thread_id: "t",
    text: "",
    created_at: "2026-01-01T00:00:00Z",
    ...partial,
  }
}

describe("compact transcript", () => {
  it("keeps interleaved manager and worker streams in separate histories", () => {
    let blocks: CompactBlock[] = []
    for (const row of [
      ev({ seq: 1, kind: "spawned", agent_id: "w1", role: "researcher", text: "secret launch instruction" }),
      ev({ seq: 2, kind: "delta", agent_id: "manager", text: "manager starts" }),
      ev({ seq: 3, kind: "delta", agent_id: "w1", text: "worker starts" }),
      ev({ seq: 4, kind: "agent_message", agent_id: "manager", text: "manager answer" }),
      ev({ seq: 5, kind: "agent_message", agent_id: "w1", text: "worker answer" }),
      ev({ seq: 6, kind: "finished", agent_id: "w1", role: "researcher", text: "worker answer" }),
    ]) blocks = applyEvent(blocks, row)
    expect(managerBlocks(blocks).filter((b) => b.kind === "answer").map((b) => b.text))
      .toEqual(["manager answer"])
    expect(phoneAgents(blocks)).toMatchObject([{
      id: "w1", role: "researcher", status: "done",
      blocks: [{ kind: "spawn" }, { kind: "answer", text: "worker answer" }],
    }])
    expect(phoneAgents(blocks)[0].blocks.filter((b) => b.kind === "answer")).toHaveLength(1)
    expect(blocks.some((b) => b.text === "secret launch instruction")).toBe(false)
  })

  it("restarts a resumed worker and marks failed finishes without creating another row", () => {
    let blocks: CompactBlock[] = []
    for (const row of [
      ev({ seq: 1, kind: "spawned", agent_id: "w1", role: "reader" }),
      ev({ seq: 2, kind: "finished", agent_id: "w1", err: "timed out" }),
      ev({ seq: 3, kind: "spawned", agent_id: "w1", role: "reader" }),
    ]) blocks = applyEvent(blocks, row)
    expect(phoneAgents(blocks)).toMatchObject([{ id: "w1", status: "running" }])
    blocks = applyEvent(blocks, ev({ seq: 4, kind: "finished", agent_id: "w1", err: "failed" }))
    expect(phoneAgents(blocks)).toMatchObject([{ id: "w1", status: "failed" }])
  })

  it("closes a worker's pending tool when that worker finishes", () => {
    let blocks = applyEvent([], ev({ seq: 1, kind: "spawned", agent_id: "w1", role: "reader" }))
    blocks = applyEvent(blocks, ev({ seq: 2, kind: "tool_call", agent_id: "w1", tool_call_id: "c1", text: "read({})" }))
    blocks = applyEvent(blocks, ev({ seq: 3, kind: "finished", agent_id: "w1", err: "stopped" }))
    expect(phoneAgents(blocks)[0].blocks.find((b) => b.kind === "tool"))
      .toMatchObject({ pending: false, failed: true })
  })
  it("splits user and assistant and streams deltas into one answer", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(blocks, ev({ seq: 1, kind: "user_message", text: "hello" }))
    blocks = applyEvent(blocks, ev({ seq: 2, kind: "delta", text: "he" }))
    blocks = applyEvent(blocks, ev({ seq: 3, kind: "delta", text: "hello" }))
    blocks = applyEvent(blocks, ev({ seq: 4, kind: "agent_message", text: "hello" }))
    expect(blocks.map((b) => b.kind)).toEqual(["user", "answer"])
    expect(blocks[1].text).toBe("hello")
    expect(blocks[1].streaming).toBe(false)
  })

  it("folds ask_user into a pending question then settles on result", () => {
    let blocks: CompactBlock[] = []
    const args =
      '{"questions":[{"id":"q1","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}'
    blocks = applyEvent(
      blocks,
      ev({
        seq: 5,
        kind: "tool_call",
        tool_call_id: "c1",
        text: `ask_user(${args})`,
      }),
    )
    const card = pendingAsk(blocks)
    expect(card?.pending).toBe(true)
    expect(card?.questions).toHaveLength(1)
    expect(card?.questions?.[0].options.some((o) => o.id === "other")).toBe(true)
    blocks = applyEvent(
      blocks,
      ev({ seq: 6, kind: "tool_result", tool_call_id: "c1", text: "{}" }),
    )
    expect(pendingAsk(blocks)).toBeUndefined()
  })

  it("keeps tool rows collapsed and spawned without a body", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({ seq: 7, kind: "tool_call", tool_call_id: "w", text: "read({})" }),
    )
    blocks = applyEvent(
      blocks,
      ev({ seq: 8, kind: "spawned", role: "researcher", text: "" }),
    )
    blocks = applyEvent(blocks, ev({ seq: 9, kind: "error", err: "boom" }))
    expect(blocks[0].kind).toBe("tool")
    expect(blocks[0].pending).toBe(true)
    expect(blocks[1].kind).toBe("spawn")
    expect(blocks[1].text).toBe("researcher")
    expect(blocks[2].kind).toBe("error")
  })

  it("does not leak unknown kinds into the transcript", () => {
    const blocks = applyEvent([], ev({ seq: 10, kind: "not_a_kind", text: "ghost" }))
    expect(blocks).toEqual([])
  })

  it("folds a live tool_delta into the pending tool row", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({ seq: 11, kind: "tool_call", tool_call_id: "c", text: "read({})" }),
    )
    blocks = applyEvent(
      blocks,
      ev({ seq: 12, kind: "tool_delta", tool_call_id: "c", text: "line" }),
    )
    expect(blocks).toHaveLength(1)
    expect(blocks[0].text).toBe("line")
  })

  it("keeps goal and plan notices without dumping empty kinds", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(blocks, ev({ seq: 13, kind: "goal", text: "keep going" }))
    blocks = applyEvent(blocks, ev({ seq: 14, kind: "plan", text: "" }))
    expect(blocks.map((b) => b.kind)).toEqual(["notice"])
  })

  it("folds schedule kinds to one-liners instead of the payload JSON", () => {
    const payload = JSON.stringify({
      id: "sch_x",
      kind: "thread",
      title: "periodic",
      prompt: "do the long check then cat status.json",
    })
    let blocks: CompactBlock[] = []
    blocks = applyEvent(blocks, ev({ seq: 20, kind: "schedule", text: payload }))
    blocks = applyEvent(
      blocks,
      ev({ seq: 21, kind: "user_message", text: "This turn is a scheduled check.\n\nkeep going" }),
    )
    blocks = applyEvent(blocks, ev({ seq: 22, kind: "schedule_fired", text: payload }))
    blocks = applyEvent(blocks, ev({ seq: 23, kind: "schedule_report", text: payload }))
    blocks = applyEvent(blocks, ev({ seq: 24, kind: "schedule_skipped", text: payload }))
    blocks = applyEvent(blocks, ev({ seq: 25, kind: "schedule_cancelled", text: "sch_x" }))
    expect(blocks.map((b) => b.kind)).toEqual(["notice", "notice", "notice"])
    expect(blocks.map((b) => b.text)).toEqual([
      "A wait is armed.",
      "Scheduled check.",
      "A wait was cancelled.",
    ])
    expect(blocks.some((b) => b.text.includes("status.json"))).toBe(false)
    expect(blocks.some((b) => b.text.includes("sch_x"))).toBe(false)
  })

  it("turns report findings into a notice and hides schedule tool names", () => {
    const args = JSON.stringify({ findings: "one thing changed", quiet: false })
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({
        seq: 30,
        kind: "tool_call",
        tool_call_id: "c",
        text: `report_schedule(${args})`,
      }),
    )
    blocks = applyEvent(
      blocks,
      ev({
        seq: 31,
        kind: "tool_result",
        tool_call_id: "c",
        text: JSON.stringify({ ok: true, quiet: false }),
      }),
    )
    expect(blocks).toHaveLength(1)
    expect(blocks[0].kind).toBe("notice")
    expect(blocks[0].text).toBe("one thing changed")
    expect(blocks[0].toolName).toBeUndefined()
    blocks = applyEvent(
      [],
      ev({
        seq: 32,
        kind: "tool_call",
        tool_call_id: "w",
        text: `schedule_wake({"every_s":30,"prompt":"Continue the wait."})`,
      }),
    )
    expect(blocks).toEqual([])
    blocks = applyEvent(
      [],
      ev({
        seq: 33,
        kind: "tool_call",
        text: `report_schedule({"findings":""})`,
      }),
    )
    expect(blocks).toEqual([])
  })

  it("does not paint a progress pulse as a notice", () => {
    const pulse = JSON.stringify({
      elapsed_ms: 0,
      agents: [{ agent_id: "w1", role: "worker", status: "done", elapsed_ms: 0 }],
    })
    const blocks = applyEvent([], ev({ seq: 40, kind: "progress", text: pulse }))
    expect(blocks).toEqual([])
  })

  it("skips packed cap and compact envelopes instead of dumping them", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({ seq: 41, kind: "max_iterations", text: JSON.stringify({ limit: 8, extend_by: 8 }) }),
    )
    blocks = applyEvent(
      blocks,
      ev({ seq: 42, kind: "compacted", text: JSON.stringify({ through_seq: 9 }) }),
    )
    blocks = applyEvent(blocks, ev({ seq: 43, kind: "goal", text: "keep going" }))
    expect(blocks.map((b) => b.kind)).toEqual(["notice"])
    expect(blocks[0].text).toBe("keep going")
    expect(blocks.some((b) => b.text.includes("elapsed_ms"))).toBe(false)
    expect(blocks.some((b) => b.text.includes("through_seq"))).toBe(false)
  })

  it("keeps a streamed thought as one block, then closes it when the answer starts", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(blocks, ev({ seq: 50, kind: "reasoning_delta", text: "first" }))
    blocks = applyEvent(blocks, ev({ seq: 51, kind: "reasoning_delta", text: "first\ncloser" }))
    blocks = applyEvent(blocks, ev({ seq: 52, kind: "reasoning", text: "first\ncloser" }))
    expect(blocks).toHaveLength(1)
    expect(blocks[0].kind).toBe("reasoning")
    expect(blocks[0].streaming).toBe(false)
    expect(blocks[0].text).toBe("first\ncloser")
    blocks = applyEvent(blocks, ev({ seq: 53, kind: "delta", text: "hi" }))
    expect(blocks.map((b) => b.kind)).toEqual(["reasoning", "answer"])
  })

  it("does not stack a snapshot that restates the answer already on screen", () => {
    let blocks = applyEvent([], ev({ seq: 60, kind: "agent_message", text: "status update" }))
    blocks = applyEvent(blocks, ev({ seq: 0, kind: "delta", text: "status update" }))
    blocks = applyEvent(blocks, ev({ seq: 0, kind: "delta", text: "status update, continued" }))
    blocks = applyEvent(blocks, ev({ seq: 61, kind: "agent_message", text: "status update, continued" }))
    expect(blocks.map((b) => b.text)).toEqual(["status update, continued"])
    blocks = applyEvent(blocks, ev({ seq: 62, kind: "agent_message", text: "a different line" }))
    expect(blocks.map((b) => b.text)).toEqual(["status update, continued", "a different line"])
  })

  it("keeps the same sentence when a tool sits between the two copies", () => {
    let blocks = applyEvent([], ev({ seq: 70, kind: "agent_message", text: "status update" }))
    blocks = applyEvent(
      blocks,
      ev({ seq: 71, kind: "tool_call", tool_call_id: "c1", text: "exec({\"cmd\":\"true\"})" }),
    )
    blocks = applyEvent(blocks, ev({ seq: 72, kind: "agent_message", text: "status update" }))
    expect(blocks.filter((b) => b.kind === "answer").map((b) => b.text)).toEqual([
      "status update",
      "status update",
    ])
  })

  it("opens a new bubble when the next delta is not the same text so far", () => {
    let blocks = applyEvent([], ev({ seq: 80, kind: "agent_message", text: "status update" }))
    blocks = applyEvent(blocks, ev({ seq: 0, kind: "delta", text: "next" }))
    expect(blocks.map((b) => b.text)).toEqual(["status update", "next"])
  })
})

describe("foldPhoneItems", () => {
  function row(partial: Partial<CompactBlock> & Pick<CompactBlock, "kind">): CompactBlock {
    return { id: partial.id ?? partial.kind, text: partial.text ?? "", ...partial }
  }

  it("keeps an answer visible and splits the fold around it", () => {
    const items = foldPhoneItems([
      row({ id: "u", kind: "user", text: "ask" }),
      row({ id: "r", kind: "reasoning", text: "looking" }),
      row({ id: "a", kind: "answer", text: "mid" }),
      row({ id: "k", kind: "tool", text: "grep", toolName: "grep" }),
      row({ id: "a2", kind: "answer", text: "done" }),
    ])
    expect(items.map((i) => i.type)).toEqual(["block", "work", "block", "work", "block"])
    expect(items[1]).toMatchObject({ type: "work", blocks: [{ id: "r" }] })
    expect(items[2]).toMatchObject({ type: "block", block: { id: "a" } })
    expect(items[3]).toMatchObject({ type: "work", blocks: [{ id: "k" }] })
  })

  it("merges adjacent thoughts and tools, skipping spawn bookkeeping", () => {
    const items = foldPhoneItems([
      row({ id: "r", kind: "reasoning", text: "looking" }),
      row({
        id: "sc",
        kind: "tool",
        toolName: "spawn_agent",
        text: "spawn_agent",
      }),
      row({ id: "sp", kind: "spawn", text: "worker" }),
      row({ id: "k", kind: "tool", text: "read", toolName: "read" }),
      row({ id: "r2", kind: "reasoning", text: "again" }),
      row({ id: "a", kind: "answer", text: "done" }),
    ])
    expect(items).toHaveLength(2)
    expect(items[0]).toMatchObject({
      type: "work",
      blocks: [{ id: "r" }, { id: "k" }, { id: "r2" }],
    })
    expect(items[1]).toMatchObject({ type: "block", block: { id: "a" } })
  })

  it("names the live tail and stays quiet once the turn is idle", () => {
    setLocale("en")
    const thought = row({
      id: "r",
      kind: "reasoning",
      text: "first\ncloser",
      streaming: true,
    })
    expect(phoneWorkTicker([thought], true)?.kind).toBe("thinking")
    expect(phoneWorkTicker([thought], true)?.detail).toBe("closer")
    expect(phoneWorkTicker([thought], false)).toBeNull()

    const read = row({
      id: "rd",
      kind: "tool",
      toolName: "read",
      text: "read",
      args: `{"file_path":"src/lib/appearance.ts"}`,
      pending: true,
    })
    expect(phoneWorkTicker([thought, read], true)).toMatchObject({
      kind: "reading",
      detail: "appearance.ts",
    })

    const idle = row({
      id: "k",
      kind: "tool",
      toolName: "read",
      text: "read",
      pending: false,
    })
    expect(phoneWorkTicker([row({ id: "r2", kind: "reasoning", text: "done" }), idle], true)?.kind).toBe(
      "planning",
    )
    expect(formatPhoneTicker({ kind: "planning", detail: "" })).toBe("Planning next moves")
    const closed = foldPhoneItems([
      row({ id: "r3", kind: "reasoning", text: "first pass" }),
      idle,
      row({ id: "a", kind: "answer", text: "landed" }),
    ])
    expect(phonePlanningTail(closed, true)).toBe(true)
    expect(phonePlanningTail(closed, false)).toBe(false)
    expect(
      phonePlanningTail(
        foldPhoneItems([
          row({ id: "r4", kind: "reasoning", text: "first pass" }),
          idle,
          row({ id: "a2", kind: "answer", text: "partial", streaming: true }),
        ]),
        true,
      ),
    ).toBe(false)
    expect(
      phonePlanningTail(
        foldPhoneItems([
          row({ id: "r5", kind: "reasoning", text: "first pass" }),
          idle,
          row({ id: "a3", kind: "answer", text: "landed" }),
          row({ id: "q", kind: "question", text: "which one" }),
        ]),
        true,
      ),
    ).toBe(false)
    expect(phonePlanningTail(foldPhoneItems([row({ id: "a4", kind: "answer", text: "only" })]), true)).toBe(
      false,
    )
    expect(
      phonePlanningTail(foldPhoneItems([row({ id: "r6", kind: "reasoning", text: "still looking" })]), true),
    ).toBe(false)
    expect(
      phonePlanningTail(
        foldPhoneItems([
          row({ id: "r7", kind: "reasoning", text: "first pass" }),
          row({ id: "p", kind: "tool", toolName: "exec", text: "exec", pending: true }),
          row({ id: "a5", kind: "answer", text: "landed" }),
        ]),
        true,
      ),
    ).toBe(false)
    expect(
      phonePlanningTail(
        foldPhoneItems([
          row({ id: "r8", kind: "reasoning", text: "first pass" }),
          idle,
          row({ id: "a6", kind: "answer", text: "landed" }),
          row({ id: "e", kind: "error", text: "nope" }),
        ]),
        true,
      ),
    ).toBe(false)
    expect(formatPhoneTicker({ kind: "thinking", detail: "" })).toBe("Thinking")
    expect(formatPhoneTicker({ kind: "exec", detail: "printf x" })).toBe("Exec printf x")
  })
})
