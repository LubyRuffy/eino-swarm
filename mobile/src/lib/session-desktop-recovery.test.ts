import { describe, expect, it } from "vitest"
import { applyPush, openView } from "./session"
import { OpEvent, OpReady, type RemoteEvent, type RemoteResponse } from "./rpc"
import { phoneAgents } from "./transcript"

describe("desktop recovery metadata on a paired phone", () => {
  for (const running of [true, false]) {
    it(`retains the original ${running ? "running" : "completed"} work across live and cold metadata`, () => {
      const base = { thread_id: "thread", turn_id: "turn", text: "", created_at: "2026-10-22T00:00:00Z" }
      const events: RemoteEvent[] = [
        { ...base, seq: 1, kind: "user_message", text: "inspect the operation" },
        { ...base, seq: 2, kind: "spawned", agent_id: "worker", role: "verifier" },
      ]
      if (!running) events.push(
        { ...base, seq: 3, kind: "finished", agent_id: "worker", text: "verified" },
        { ...base, seq: 4, kind: "done" },
      )
      const ready = (rows: RemoteEvent[]): RemoteResponse => ({
        v: 1, id: "watch", ok: true, op: OpReady, thread_id: "thread", seq: rows.at(-1)!.seq,
        status: { running, turn_id: running ? "turn" : undefined }, events: rows,
      })
      const open = () => openView({ id: "thread", title: "work" })
      const before = applyPush(open(), ready(events))
      const diagnostic: RemoteEvent = { ...base, seq: 5, kind: "desktop_recovery", text: '{"session_id":"renderer-session","phase":"recovered"}' }
      const pushed: RemoteResponse = { v: 1, id: "", ok: true, op: OpEvent, thread_id: "thread", seq: 5, event: diagnostic }
      const live = applyPush(before, pushed)
      expect(live.blocks).toEqual(before.blocks)
      expect(live.detail).toEqual(before.detail)
      expect(phoneAgents(live.blocks)).toEqual(phoneAgents(before.blocks))
      expect(phoneAgents(live.blocks).map((agent) => agent.id)).toEqual(["worker"])
      expect(live.lastSeq).toBe(5)
      expect(applyPush(live, pushed)).toEqual(live)
      const cold = applyPush(open(), ready([...events, diagnostic]))
      expect(cold.blocks).toEqual(before.blocks)
      expect(cold.detail).toEqual(before.detail)
    })
  }
})
