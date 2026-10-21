import { describe, expect, it } from "vitest"

import { KINDS } from "./stream"
import { emptyTranscript, liveWorkers, MANAGER_ID, reduceEvent, rewindTranscript, type ReduceMode } from "./transcript"
import type { SwarmEvent } from "./types"

const failure = "scheduled check ended without executing a tool or reporting its result"
const owner = {
  reason: "scheduled_check_failed",
  owner_kind: "thread_wake",
  owner_id: "wake-1",
  worker_ids: ["worker-1", "worker-2"],
}

function event(kind: string, seq: number, patch: Partial<SwarmEvent> = {}): SwarmEvent {
  return {
    kind, seq, thread_id: "thread", turn_id: "check", agent_id: MANAGER_ID,
    created_at: `2026-10-21T09:00:${String(seq).padStart(2, "0")}Z`, ...patch,
  }
}

function started(): SwarmEvent[] {
  return [
    event("schedule_fired", 1),
    event("spawned", 2, { agent_id: "worker-1", role: "writer" }),
    event("spawned", 3, { agent_id: "worker-2", role: "reviewer" }),
    event("tool_call", 4, { agent_id: "worker-1", text: 'exec({"command":"check"})', tool_call_id: "call-1" }),
    event("reasoning_delta", 0, { agent_id: "worker-2", text: "Checking the assigned evidence." }),
  ]
}

function receipt(seq = 5, payload: unknown = owner): SwarmEvent {
  return event("workers_retained", seq, { text: JSON.stringify(payload) })
}

function fold(events: SwarmEvent[], mode: ReduceMode = "full") {
  return events.reduce((state, ev) => reduceEvent(state, ev, mode), emptyTranscript())
}

describe("scheduled worker ownership", () => {
  it("subscribes to the durable retention receipt before the terminal error", () => {
    expect(KINDS).toContain("workers_retained")
  })

  describe.each(["full", "roster"] as const)("%s transcript", (mode) => {
    // A failed manager check cannot turn an independently owned live exec
    // into a failure. Reload must reconstruct the same running worker.
    it.each(["goal", "thread_wake"])("retains explicit workers under their %s owner in live events and replay", (kind) => {
      const beforeError = [...started(), receipt(5, { ...owner, owner_kind: kind })]
      const failed = event("error", 6, { err: failure })
      const before = fold(beforeError, mode)
      const live = reduceEvent(before, failed, mode)
      const replay = fold([...beforeError, failed], mode)
      expect(live).toEqual(replay)
      for (const state of [live, replay]) {
        expect(state.running).toBe(false)
        expect(state.turns.find((turn) => turn.id === "check")).toMatchObject({ status: "error", error: failure })
        expect(liveWorkers(state)).toBe(2)
        expect(state.agents["worker-1"]).toMatchObject({ status: "running" })
        expect(state.agents["worker-1"].blocks.find((block) => block.tool?.callId === "call-1")?.tool).toMatchObject({ pending: true })
        expect(state.agents["worker-2"].blocks.find((block) => block.kind === "reasoning")?.streaming).toBe(true)
        expect(state.agents["worker-1"].error).toBeUndefined()
        expect(state.agents["worker-1"].errorSource).toBeUndefined()
        expect(state.agents[MANAGER_ID].blocks.some((block) => block.text.includes("owner_kind"))).toBe(false)
      }
      expect(before.agents["worker-1"].status).toBe("running")
    })

    it("does not infer retention from the scheduled error text without a receipt", () => {
      const state = fold([...started(), event("error", 6, { err: failure })], mode)
      expect(liveWorkers(state)).toBe(0)
      expect(state.agents["worker-1"]).toMatchObject({ status: "failed", errorSource: "turn", error: failure })
      expect(state.agents["worker-1"].blocks.find((block) => block.tool)?.tool?.pending).toBe(false)
    })

    it("retains only the listed workers and only across that check's error", () => {
      const retained = fold([
        ...started(), receipt(5, { ...owner, worker_ids: ["worker-1"] }),
        event("spawned", 6, { agent_id: "worker-3", role: "verifier" }),
        event("error", 7, { err: failure }),
      ], mode)
      expect(retained.agents["worker-1"].status).toBe("running")
      expect(retained.agents["worker-2"].status).toBe("failed")
      expect(retained.agents["worker-3"].status).toBe("failed")
      const next = reduceEvent(retained, event("error", 8, { turn_id: "next-check", err: "another failure" }), mode)
      expect(next.agents["worker-1"]).toMatchObject({ status: "failed", error: "another failure" })
    })

    it.each(["cleanup", "interrupted"])("keeps %s authoritative over retained workers", (stop) => {
      const events = [...started(), receipt(), event("error", 6, { err: failure })]
      if (stop === "cleanup") events.push(event("cleanup", 7, { text: "Stopped unfinished workers." }))
      else events.push(event("error", 7, { err: "interrupted" }))
      const state = fold(events, mode)
      expect(liveWorkers(state)).toBe(0)
      expect(state.agents["worker-1"].status).toBe("cancelled")
      expect(state.agents["worker-1"].blocks.find((block) => block.tool)?.tool?.pending).toBe(false)
      expect(state.agents["worker-2"].blocks.find((block) => block.kind === "reasoning")?.streaming).toBe(false)
    })

    it.each([undefined, "worker's own failure"])("keeps an independent finished receipt authoritative (%s)", (err) => {
      const state = fold([
        ...started(), receipt(),
        event("finished", 6, { agent_id: "worker-1", err, text: "Verified result." }),
        event("error", 7, { err: failure }),
      ], mode)
      expect(state.agents["worker-1"]).toMatchObject({ status: err ? "failed" : "done", result: "Verified result." })
      expect(state.agents["worker-1"].error).toBe(err)
      expect(state.agents["worker-2"].status).toBe("running")
    })

    it("allows retained workers to finish naturally after the failed check", () => {
      const state = fold([
        ...started(), receipt(), event("error", 6, { err: failure }),
        event("tool_result", 7, { agent_id: "worker-1", tool_call_id: "call-1", text: "Verified evidence." }),
        event("finished", 8, { agent_id: "worker-1", text: "Work complete." }),
      ], mode)
      expect(state.agents["worker-1"]).toMatchObject({ status: "done", result: "Work complete." })
      expect(state.agents["worker-1"].blocks.find((block) => block.tool)?.tool).toMatchObject({ pending: false, result: "Verified evidence." })
      expect(state.turns.find((turn) => turn.id === "check")?.status).toBe("error")
    })

    it("does not revive cleanup-cancelled workers even if a late retention receipt and error arrive", () => {
      const state = fold([
        ...started(), event("cleanup", 5), receipt(6), event("error", 7, { err: failure }),
        event("finished", 8, { agent_id: "worker-1", err: "context canceled" }),
      ], mode)
      expect(state.agents["worker-1"].status).toBe("cancelled")
      expect(liveWorkers(state)).toBe(0)
    })
  })

  it.each([
    null, [], {}, { ...owner, reason: "unknown" }, { ...owner, owner_kind: "task" },
    { ...owner, owner_id: "" }, { ...owner, worker_ids: undefined },
    { ...owner, worker_ids: "worker-1" }, { ...owner, worker_ids: ["worker-1", 2] },
    { ...owner, worker_ids: [MANAGER_ID] }, { ...owner, worker_ids: [""] },
  ])("does not grant retention for an invalid receipt: %j", (payload) => {
    const state = fold([...started(), receipt(5, payload), event("error", 6, { err: failure })])
    expect(liveWorkers(state)).toBe(0)
    expect(state.agents["worker-1"].status).toBe("failed")
  })

  it.each([
    { agent_id: "worker-2" }, { turn_id: "other-check" }, { turn_id: "" }, { seq: 0 },
  ])("does not let a receipt escape its durable manager-turn identity: %j", (patch) => {
    const state = fold([...started(), { ...receipt(), ...patch }, event("error", 6, { err: failure })])
    expect(liveWorkers(state)).toBe(0)
  })

  it("does not accept a receipt after a terminal error or revive workers", () => {
    const state = fold([...started(), event("error", 5, { err: failure }), receipt(6)])
    expect(liveWorkers(state)).toBe(0)
    expect(state.turns.find((turn) => turn.id === "check")?.status).toBe("error")
    expect(state.agents[MANAGER_ID].blocks.some((block) => block.text.includes("owner_kind"))).toBe(false)
  })

  it("does not let a later receipt authorize an older terminal error", () => {
    const state = fold([...started(), receipt(8), event("error", 7, { err: failure })])
    expect(liveWorkers(state)).toBe(0)
  })

  it("binds retention to the receipt instead of matching a provider error's words", () => {
    const state = fold([...started(), receipt(), event("error", 6, { err: "upstream model returned an error" })])
    expect(liveWorkers(state)).toBe(2)
    expect(state.turns[0].error).toBe("upstream model returned an error")
  })

  it("drops a deleted ownership receipt during rewind and preserves an earlier receipt", () => {
    const state = fold([...started(), receipt(), event("error", 6, { err: failure })])
    expect(rewindTranscript(state, 5).turns[0].workerRetention).toBeUndefined()
    expect(rewindTranscript(state, 6).turns[0].workerRetention?.workerIds).toEqual(owner.worker_ids)
  })

  it("does not mutate the running state while settling an unretained worker", () => {
    const before = fold(started())
    const after = reduceEvent(before, event("error", 6, { err: "ordinary failure" }))
    expect(after.agents["worker-1"].status).toBe("failed")
    expect(before.agents["worker-1"].status).toBe("running")
    expect(before.agents["worker-1"].blocks.find((block) => block.tool)?.tool?.pending).toBe(true)
  })
})
