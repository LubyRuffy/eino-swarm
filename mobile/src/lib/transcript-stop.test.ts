import { describe, expect, it } from "vitest"

import { OpEvent, OpReady, type RemoteEvent } from "./rpc"
import { applyPush, openView, prependOlder } from "./session"
import { applyEvent, phoneAgents, type CompactBlock } from "./transcript"

type StopOrder = "before_cleanup" | "after_cleanup" | "after_terminal"

function events(order: StopOrder): RemoteEvent[] {
  const finished = ["reader-1", "writer-2"].map((agent_id) => ({
    kind: "finished", agent_id, err: `swarm: agent ${agent_id} cancelled: context canceled`,
  }))
  const cleanup = { kind: "cleanup", text: "Stopped unfinished workers." }
  const terminal = { kind: "error", err: "interrupted" }
  const endings = {
    before_cleanup: [...finished, cleanup, terminal],
    after_cleanup: [cleanup, ...finished, terminal],
    after_terminal: [cleanup, terminal, ...finished],
  }
  return [
    { kind: "user_message", text: "Continue the assigned work." },
    ...["reader-1", "writer-2", "done-3", "failed-4"].map((agent_id) => ({ kind: "spawned", agent_id, role: "worker" })),
    { kind: "finished", agent_id: "done-3", text: "Verified result." },
    { kind: "finished", agent_id: "failed-4", err: "provider unavailable" },
    { kind: "tool_call", agent_id: "reader-1", tool_call_id: "read", text: "read({})" },
    { kind: "user_stop_requested", text: "Stop requested by the user." },
    ...endings[order],
  ].map((row, index) => ({ thread_id: "thread", turn_id: "turn", text: "", agent_id: "manager",
    created_at: "2026-01-01T00:00:00Z", seq: index + 1, ...row }))
}

function fold(rows: RemoteEvent[]): CompactBlock[] {
  return rows.reduce<CompactBlock[]>(applyEvent, [])
}

describe("worker Stop and phone replay", () => {
  // Real cancellation callbacks race cleanup; Stop must not turn red on reconnect.
  it.each<StopOrder>(["before_cleanup", "after_cleanup", "after_terminal"])("keeps cancellations stopped %s, live and after rewatch/paging", (order) => {
    const rows = events(order)
    const detail = { id: "thread", title: "Work" }
    let live = applyPush(openView(detail), { v: 1, id: "", ok: true, op: OpReady, thread_id: "thread", events: [], seq: 0 })
    for (const event of rows) live = applyPush(live, { v: 1, id: "", ok: true, op: OpEvent, thread_id: "thread", event })
    const replay = applyPush(openView(detail), { v: 1, id: "", ok: true, op: OpReady, thread_id: "thread", events: rows, seq: rows.at(-1)!.seq })
    const tail = applyPush(openView(detail), { v: 1, id: "", ok: true, op: OpReady, thread_id: "thread", events: rows.slice(-3), more: true })
    const paged = prependOlder(tail, rows.slice(0, -3), false)
    for (const blocks of [live.blocks, replay.blocks, paged.blocks]) {
      const agents = phoneAgents(blocks)
      for (const id of ["reader-1", "writer-2"]) {
        const agent = agents.find((row) => row.id === id)!
        expect(agent.status).toBe("cancelled")
        expect(agent.blocks.every((block) => !block.pending && !block.streaming && !block.failed)).toBe(true)
      }
      expect(agents.find((agent) => agent.id === "done-3")?.status).toBe("done")
      expect(agents.find((agent) => agent.id === "failed-4")?.status).toBe("failed")
    }
  })

  it("marks cleanup-only workers stopped and preserves genuine finished results", () => {
    const blocks = fold(events("after_terminal").filter((event) => event.kind !== "finished" || ["done-3", "failed-4"].includes(event.agent_id!)))
    expect(phoneAgents(blocks).map(({ id, status }) => [id, status])).toEqual([
      ["reader-1", "cancelled"], ["writer-2", "cancelled"], ["done-3", "done"], ["failed-4", "failed"],
    ])
  })

  it.each([
    "context canceled",
    "provider cancelled the response",
    "swarm: agent reader-10 cancelled: context canceled",
    "swarm: agent another-1 cancelled: context canceled",
    "swarm: agent reader-1 cancelled: ",
    "swarm: agent reader-1 exceeded timeout 1m0s: context deadline exceeded",
  ])("does not relabel another error as cancellation: %s", (err) => {
    const blocks = fold([{ thread_id: "thread", seq: 1, kind: "finished", agent_id: "reader-1", text: "", err, created_at: "" }])
    expect(phoneAgents(blocks)[0].status).toBe("failed")
  })

  it("preserves diagnostics and lets the same worker resume and finish normally", () => {
    const rows = events("after_terminal")
    const stopped = fold(rows)
    expect(stopped.find((block) => block.agentId === "reader-1" && block.text.includes("cancelled:"))?.text)
      .toBe("swarm: agent reader-1 cancelled: context canceled")
    const resumed = fold([...rows,
      { thread_id: "thread", seq: 30, kind: "spawned", agent_id: "reader-1", role: "reader", text: "", created_at: "" },
      { thread_id: "thread", seq: 31, kind: "finished", agent_id: "reader-1", text: "Confirmed output.", created_at: "" },
    ])
    expect(phoneAgents(resumed).find((agent) => agent.id === "reader-1")?.status).toBe("done")
  })

  it("shows human interruption as a notice while a model error stays an error", () => {
    const event = { thread_id: "thread", seq: 1, kind: "error", agent_id: "manager", text: "", created_at: "" }
    expect(fold([{ ...event, err: "interrupted" }])[0]).toMatchObject({ kind: "notice", text: "interrupted" })
    expect(fold([{ ...event, err: "provider unavailable" }])[0]).toMatchObject({ kind: "error", text: "provider unavailable" })
  })
})
