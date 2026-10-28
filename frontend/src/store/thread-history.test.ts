import { describe, expect, it } from "vitest"

import { MANAGER_ID, reduceEvent } from "@/lib/transcript"
import {
  applyAgentLog,
  applyTail,
  historyNewestSeq,
  historyOldestSeq,
  prependOlder,
  rememberRewind,
  rememberStored,
  resetThreadHistory,
  rewindStoredTranscript,
} from "./thread-history"
import type { SwarmEvent } from "@/lib/types"

function ev(seq: number, text: string, turnId = "tn_a"): SwarmEvent {
  return {
    thread_id: "th_1",
    turn_id: turnId,
    seq,
    kind: "user_message",
    agent_id: MANAGER_ID,
    text,
    created_at: new Date().toISOString(),
  }
}

describe("thread history buffer", () => {
  it("rebuilds the transcript from a tail page then older rows", () => {
    resetThreadHistory()
    const tail = applyTail("th_1", [ev(4, "later", "tn_b")])
    expect(tail.agents[MANAGER_ID]?.blocks.map((b) => b.text)).toEqual(["later"])
    expect(historyOldestSeq()).toBe(4)
    expect(historyNewestSeq()).toBe(4)

    const full = prependOlder("th_1", [ev(2, "earlier")])
    expect(full?.agents[MANAGER_ID]?.blocks.map((b) => b.text)).toEqual([
      "earlier",
      "later",
    ])
    expect(historyOldestSeq()).toBe(2)
  })

  it("ignores a prepend for a conversation the reader already left", () => {
    resetThreadHistory()
    applyTail("th_1", [ev(4, "later")])
    expect(prependOlder("th_other", [ev(2, "earlier")])).toBeUndefined()
  })

  it("drops stored rows on rewind so a later prepend cannot resurrect them", () => {
    resetThreadHistory()
    applyTail("th_1", [ev(2, "keep"), ev(4, "gone")])
    rememberRewind("th_1", 4)
    expect(historyNewestSeq()).toBe(2)
    rememberStored("th_1", ev(5, "next", "tn_c"))
    expect(historyNewestSeq()).toBe(5)
    rememberRewind("th_1", 4, 9)
    expect(historyNewestSeq()).toBe(2)
    rememberStored("th_1", ev(6, "ghost", "tn_b"))
    expect(historyNewestSeq()).toBe(2)
    rememberStored("th_1", ev(10, "after", "tn_d"))
    expect(historyNewestSeq()).toBe(10)
  })

  it("leaves the manager empty when the live-edge page is only worker tool rows", () => {
    resetThreadHistory()
    const state = applyTail("th_1", [
      {
        thread_id: "th_1",
        turn_id: "tn_a",
        seq: 40,
        kind: "tool_call",
        agent_id: "worker-1",
        role: "worker",
        text: "exec({})",
        tool_call_id: "c1",
        created_at: new Date().toISOString(),
      },
    ])
    expect(state.agents[MANAGER_ID]?.blocks ?? []).toHaveLength(0)
    expect(state.agents["worker-1"]?.blocks).toHaveLength(1)
  })

  it("keeps workers from a roster sidecar without moving the paging cursor", () => {
    resetThreadHistory()
    const spawned: SwarmEvent = {
      thread_id: "th_1",
      turn_id: "tn_a",
      seq: 2,
      kind: "spawned",
      agent_id: "worker-1",
      role: "worker",
      text: "do the assigned work",
      created_at: new Date().toISOString(),
    }
    const finished: SwarmEvent = {
      thread_id: "th_1",
      turn_id: "tn_a",
      seq: 8,
      kind: "finished",
      agent_id: "worker-1",
      role: "worker",
      text: "done",
      created_at: new Date().toISOString(),
    }
    const state = applyTail("th_1", [ev(40, "still going")], [spawned, finished])
    expect(historyOldestSeq()).toBe(40)
    expect(state.agentOrder).toEqual([MANAGER_ID, "worker-1"])
    expect(state.agents["worker-1"]?.status).toBe("done")
    expect(state.agents["worker-1"]?.instruction).toBe("do the assigned work")
    expect(JSON.stringify(state)).not.toMatch(/notes\.md|summarize|look into this/)
    expect(state.agents[MANAGER_ID]?.blocks.filter((b) => b.kind === "spawn")).toEqual([])

    const after = prependOlder("th_1", [ev(12, "mid")])
    expect(historyOldestSeq()).toBe(12)
    expect(after?.agents["worker-1"]?.status).toBe("done")
  })

  it("drops roster rows on rewind so a rebuild cannot resurrect them", () => {
    resetThreadHistory()
    const spawned: SwarmEvent = {
      thread_id: "th_1",
      turn_id: "tn_a",
      seq: 5,
      kind: "spawned",
      agent_id: "worker-1",
      role: "worker",
      created_at: new Date().toISOString(),
    }
    applyTail("th_1", [ev(9, "later")], [spawned])
    rememberRewind("th_1", 5)
    const after = prependOlder("th_1", [ev(2, "earlier")])
    expect(after?.agents["worker-1"]).toBeUndefined()
    expect(after?.agents[MANAGER_ID]?.blocks.map((b) => b.text)).toEqual(["earlier"])
  })

  it("paints Started once the spawn row is on a history page", () => {
    resetThreadHistory()
    const spawned: SwarmEvent = {
      thread_id: "th_1",
      turn_id: "tn_a",
      seq: 2,
      kind: "spawned",
      agent_id: "worker-1",
      role: "worker",
      created_at: new Date().toISOString(),
    }
    const tail = applyTail("th_1", [ev(9, "later")], [spawned])
    expect(tail.agents[MANAGER_ID]?.blocks.filter((b) => b.kind === "spawn")).toEqual([])

    const filled = prependOlder("th_1", [spawned, ev(4, "mid")])
    const spawns = filled?.agents[MANAGER_ID]?.blocks.filter((b) => b.kind === "spawn")
    expect(spawns).toHaveLength(1)
    expect(spawns?.[0]?.spawn?.agentId).toBe("worker-1")
    expect(historyOldestSeq()).toBe(2)
  })

  it("fills a worker's tools from an on-demand log without moving the paging cursor", () => {
    resetThreadHistory()
    const spawned: SwarmEvent = {
      thread_id: "th_1",
      turn_id: "tn_a",
      seq: 2,
      kind: "spawned",
      agent_id: "worker-1",
      role: "worker",
      created_at: new Date().toISOString(),
    }
    applyTail("th_1", [ev(40, "still going")], [spawned])
    const tool: SwarmEvent = {
      thread_id: "th_1",
      turn_id: "tn_a",
      seq: 6,
      kind: "tool_call",
      agent_id: "worker-1",
      role: "worker",
      text: "exec({})",
      tool_call_id: "c1",
      created_at: new Date().toISOString(),
    }
    const state = applyAgentLog("th_1", [tool])
    expect(historyOldestSeq()).toBe(40)
    expect(state?.agents["worker-1"]?.blocks).toHaveLength(1)
    expect(state?.agents[MANAGER_ID]?.blocks.filter((b) => b.kind === "spawn")).toEqual([])
  })

  // A rewind from another client, or an edit of the first visible message, can
  // remove every loaded row. The rows before the cut are still stored; an
  // empty page used to read as the start of the log, so they never loaded.
  for (const [label, cut] of [["below the page", 17], ["at the page's first row", 23]] as const) {
    it(`keeps older history loadable after a rewind ${label}`, () => {
      resetThreadHistory()
      const tail = applyTail("th_1", [ev(23, "visible", "tn_c"), ev(24, "later", "tn_c")])
      rewindStoredTranscript("th_1", cut, tail)
      expect(historyOldestSeq()).toBe(cut)
      const older = prependOlder("th_1", [ev(16, "kept", "tn_b")])
      expect(older?.agents[MANAGER_ID]?.blocks.map((b) => b.text)).toEqual(["kept"])
      expect(historyOldestSeq()).toBe(16)
    })
  }

  // A worker log can outrun SSE and return rows stored after another client's
  // truncation. Their seqs are not evidence of what the rewind removed.
  it("accepts the replacement turn when a worker log already holds its rows", () => {
    resetThreadHistory()
    const worker = (seq: number, kind: string, turn: string, text: string): SwarmEvent => ({
      thread_id: "th_1", turn_id: turn, seq, kind, agent_id: "worker-1", role: "worker", text,
      ...(kind === "agent_message" ? { message_id: `m_${seq}` } : {}), created_at: new Date().toISOString(),
    })
    let state = applyTail("th_1", [ev(10, "original", "tn_a"), worker(11, "spawned", "tn_a", "task")])
    state = applyAgentLog("th_1", [worker(11, "spawned", "tn_a", "task"), worker(21, "spawned", "tn_b", "task"), worker(22, "agent_message", "tn_b", "replacement result")], state)!
    state = rewindStoredTranscript("th_1", 10, state)
    for (const row of [ev(20, "resent", "tn_b"), worker(21, "spawned", "tn_b", "task"), worker(22, "agent_message", "tn_b", "replacement result")]) {
      if (rememberStored("th_1", row)) state = reduceEvent(state, row)
    }
    expect(state.agents[MANAGER_ID]?.blocks.map((b) => [b.kind, b.text])).toEqual([["user", "resent"], ["spawn", "task"]])
    expect(state.agents["worker-1"]?.blocks.map((b) => b.text)).toEqual(["replacement result"])
    expect(state.turns.map((t) => t.id)).toEqual(["tn_b"])
  })

  it("counts a worker resumed inside the loaded page as part of that turn", () => {
    resetThreadHistory()
    const resumed: SwarmEvent = {
      thread_id: "th_1", turn_id: "tn_b", seq: 15, kind: "spawned", agent_id: "worker-1", role: "worker",
      text: "continue", created_at: new Date().toISOString(),
    }
    applyTail("th_1", [ev(10, "first", "tn_a"), ev(14, "second", "tn_b")])
    const state = applyAgentLog("th_1", [resumed])
    expect(rememberStored("th_1", resumed)).toBe(false)
    expect(state?.turns.find((t) => t.id === "tn_b")?.agentIds).toEqual(["worker-1"])
  })
})

