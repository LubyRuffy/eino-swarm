import { describe, expect, it } from "vitest"
import { emptyTranscript, parseIterationLimit, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"
import { localizeNotice } from "./i18n"

const event = (seq: number, kind: string, text = "", turn = "turn-1"): SwarmEvent => ({
  thread_id: "thread-1", turn_id: turn, agent_id: "manager", seq, kind, text,
  created_at: "2026-01-01T00:00:00Z",
})
const waiting = event(2, "max_iterations", JSON.stringify({
  reason: "workers_uncollected", wait_id: "wait-1", limit: 80, extend_by: 2000, automatic_resume: true,
}))

describe("worker collection pauses", () => {
  it("keeps an explicit decline cancelled instead of presenting a model failure", () => {
    const stopped = { ...event(3, "error"), err: "stopped before collecting worker results" }
    const state = reduceEvents(emptyTranscript(), [event(1, "user_message"), waiting, stopped])
    expect(state.turns[0].status).toBe("cancelled")
    expect(state.agents.manager.blocks.filter((block) => block.kind === "error")).toHaveLength(0)
    expect(localizeNotice(stopped.err, "zh")).toBe("已停止，委派结果未收齐。")
  })
  it("keeps the reason separate from the iteration allowance", () => {
    expect(parseIterationLimit(waiting)).toMatchObject({ reason: "workers_uncollected", automaticResume: true })
    const state = reduceEvents(emptyTranscript(), [event(1, "user_message", "Inspect the inputs."), waiting])
    expect(state.agents.manager.blocks.find((block) => block.kind === "confirm")?.confirm)
      .toMatchObject({ reason: "workers_uncollected", automaticResume: true, pending: true })
  })

  it("settles the matching wait on automatic completion through live and cold replay", () => {
    const resumed = event(3, "workers_collection_resumed", '{"wait_id":"wait-1"}')
    const before = reduceEvents(emptyTranscript(), [event(1, "user_message"), waiting])
    const live = reduceEvents(before, [resumed, resumed])
    const cold = reduceEvents(emptyTranscript(), [event(1, "user_message"), waiting, resumed])
    for (const state of [live, cold]) {
      const cards = state.agents.manager.blocks.filter((block) => block.kind === "confirm")
      expect(cards).toHaveLength(1)
      expect(cards[0].confirm).toMatchObject({ pending: false, continued: true })
      expect(state.running).toBe(true)
    }
  })

  it("does not use another turn's completion or an ordinary cap as worker wake permission", () => {
    const other = event(3, "workers_collection_resumed", '{"wait_id":"wait-1"}', "other-turn")
    const waitingState = reduceEvents(emptyTranscript(), [event(1, "user_message"), waiting, other])
    expect(waitingState.agents.manager.blocks.find((block) => block.kind === "confirm")?.confirm?.pending).toBe(true)
    const cap = event(2, "max_iterations", '{"limit":8,"extend_by":8}')
    const state = reduceEvents(emptyTranscript(), [event(1, "user_message"), cap, event(3, "workers_collection_resumed")])
    expect(state.agents.manager.blocks.find((block) => block.kind === "confirm")?.confirm?.pending).toBe(true)
  })

  it("does not settle another wait in the same turn with a late completion receipt", () => {
    const state = reduceEvents(emptyTranscript(), [event(1, "user_message"), waiting,
      event(3, "workers_collection_resumed", '{"wait_id":"previous-wait"}'),
      event(4, "workers_collection_resumed", '{'),
    ])
    expect(state.agents.manager.blocks.find((block) => block.kind === "confirm")?.confirm?.pending).toBe(true)
  })

  it("updates an existing wait when recovery or changed identities remove automatic permission", () => {
    const changed = event(5, "max_iterations", '{"reason":"workers_uncollected","wait_id":"wait-1","automatic_resume":false}')
    const state = reduceEvents(emptyTranscript(), [event(1, "user_message"), waiting, changed])
    const cards = state.agents.manager.blocks.filter((block) => block.kind === "confirm")
    expect(cards).toHaveLength(1)
    expect(cards[0].confirm).toMatchObject({ automaticResume: false, pending: true })
    const rewound = reduceEvents(emptyTranscript(), [event(1, "user_message"), changed, waiting,
      event(3, "workers_collection_resumed", '{"wait_id":"wait-1"}')])
    expect(rewound.agents.manager.blocks.find((block) => block.kind === "confirm")?.confirm)
      .toMatchObject({ automaticResume: false, pending: true })
  })
})
