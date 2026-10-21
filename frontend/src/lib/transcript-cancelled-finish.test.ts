import { describe, expect, it } from "vitest"
import { emptyTranscript, liveWorkers, MANAGER_ID, reduceEvent, reduceEvents } from "./transcript"
import { mergeLogEvents } from "./thread-log"
import type { SwarmEvent } from "./types"

function history(order: "before-cleanup" | "after-cleanup" | "after-terminal"): SwarmEvent[] {
  const cancelled = ["worker-1", "worker-2"].map((id) => ({
    kind: "finished", agent_id: id, err: `swarm: agent ${id} cancelled: context canceled`,
  }))
  const cleanup = { kind: "cleanup", text: "Stopped unfinished workers." }
  const terminal = { kind: "error", err: "interrupted" }
  const ending = order === "before-cleanup" ? [...cancelled, cleanup, terminal]
    : order === "after-cleanup" ? [cleanup, ...cancelled, terminal]
      : [cleanup, terminal, ...cancelled]
  return [
    { kind: "user_message", text: "Continue the assigned work." },
    ...[1, 2, 3, 4].map((n) => ({ kind: "spawned", agent_id: `worker-${n}`, role: "worker" })),
    { kind: "finished", agent_id: "worker-3", text: "Verified result." },
    { kind: "finished", agent_id: "worker-4", err: "provider unavailable" },
    { kind: "tool_call", agent_id: "worker-1", text: "exec({})", tool_call_id: "call" },
    { kind: "delta", agent_id: "worker-2", text: "Partial progress" },
    { kind: "user_stop_requested", text: "Stop requested by the user." },
    ...ending,
  ].map((event, index) => ({
    thread_id: "thread", turn_id: "turn", agent_id: MANAGER_ID,
    created_at: `2026-10-20T01:00:${String(index).padStart(2, "0")}Z`, seq: index + 1, ...event,
  }))
}

describe("worker cancellation finish receipts", () => {
  // Cleanup does not wait for worker goroutines. Their real finish receipts
  // can precede cleanup or arrive after the manager's interrupted terminal.
  it.each(["before-cleanup", "after-cleanup", "after-terminal"] as const)(
    "retains stopped workers with cancellation finishes %s, including cold and paged replay", (order) => {
      const events = history(order)
      const live = reduceEvents(reduceEvents(emptyTranscript(), events.slice(0, 10)), events.slice(10))
      const cold = reduceEvents(emptyTranscript(), events)
      const page = events.slice(-3)
      const inPage = new Set(page.map((event) => event.seq))
      const roster = events.filter((event) => ["spawned", "finished", "cleanup"].includes(event.kind))
      const paged = mergeLogEvents(roster, page).reduce((state, event) =>
        reduceEvent(state, event, inPage.has(event.seq) ? "full" : "roster"), emptyTranscript())
      expect(cold).toEqual(live)
      for (const state of [live, cold, paged]) {
        for (const id of ["worker-1", "worker-2"]) {
          expect(state.agents[id]).toMatchObject({ status: "cancelled", activity: "stopped" })
          expect(state.agents[id].error).toBeUndefined()
          expect(state.agents[id].blocks.every((block) => !block.streaming && !block.tool?.pending)).toBe(true)
        }
        expect(state.agents["worker-3"]).toMatchObject({ status: "done", result: "Verified result." })
        expect(state.agents["worker-4"]).toMatchObject({ status: "failed", error: "provider unavailable" })
        expect(liveWorkers(state)).toBe(0)
      }
      expect(cold.turns[0].status).toBe("cancelled")
    },
  )

  it.each([
    "context canceled",
    "provider cancelled the response",
    "swarm: agent worker-2 cancelled: context canceled",
    "swarm: agent worker-10 cancelled: context canceled",
    "swarm: agent worker-1 cancelled:",
    "swarm: agent worker-1 cancelled: ",
    "swarm: agent worker-1 cancelled:   ",
    "swarm: agent worker-1 exceeded timeout 1m0s (1 consecutive watchdog failures): context deadline exceeded",
  ])("does not turn an unrelated error into a stopped worker: %s", (err) => {
    const events = history("before-cleanup").slice(0, 5)
    const finished: SwarmEvent = { ...events[1], kind: "finished", seq: 6, err }
    const state = reduceEvents(emptyTranscript(), [...events, finished])
    expect(state.agents["worker-1"]).toMatchObject({ status: "failed", activity: "failed", error: err })
  })

  it("classifies a worker cancellation without needing the manager's terminal event", () => {
    const events = history("before-cleanup")
    const state = reduceEvents(emptyTranscript(), events.slice(0, 11))
    expect(state.agents["worker-1"]).toMatchObject({ status: "cancelled", activity: "stopped" })
    expect(state.agents["worker-2"].status).toBe("running")
    expect(state.running).toBe(true)
  })

  it("lets a later dispatch of the same worker complete successfully", () => {
    const events = history("after-terminal")
    const spawned: SwarmEvent = { ...events[1], seq: 15, kind: "spawned", turn_id: "next-turn" }
    const running = reduceEvent(reduceEvents(emptyTranscript(), events), spawned)
    expect(running.agents["worker-1"]).toMatchObject({ status: "running" })
    expect(running.agents["worker-1"].error).toBeUndefined()
    const completed = reduceEvent(running, { ...spawned, seq: 16, kind: "finished", text: "Later result." })
    expect(completed.agents["worker-1"]).toMatchObject({ status: "done", result: "Later result." })
  })
})
