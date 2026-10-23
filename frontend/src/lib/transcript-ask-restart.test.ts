import { describe, expect, it } from "vitest"
import { emptyTranscript, MANAGER_ID, reduceEvent, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"

const args = JSON.stringify({ questions: [{ id: "path", prompt: "Choose a path", options: [{ id: "one", label: "First" }, { id: "two", label: "Second" }] }] })
function event(kind: string, seq: number, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return { kind, seq, thread_id: "thread", turn_id: "turn", agent_id: MANAGER_ID, tool_call_id: "ask-original", created_at: "2026-10-23T12:00:00Z", text: "", ...extra }
}
const call = event("tool_call", 2, { text: `ask_user(${args})` })
const unknown = event("tool_result", 3, { text: '{"outcome":"unknown","error":"outcome_unknown"}' })
const recovered = event("ask_recovered", 5, { text: args })
const answer = event("tool_result", 6, { text: '{"answers":{"path":{"answers":["First"]}}}' })
const card = (events: SwarmEvent[]) => reduceEvents(emptyTranscript(), events).agents.manager?.blocks.filter((b) => b.question)

describe("the original human question across engine replacement", () => {
  it.each([false, true])("rearms only the explicit durable recovery with checkpoint first=%s", (checkpointFirst) => {
    const checkpoint = event("engine_restart_checkpoint", checkpointFirst ? 3 : 4, { text: '{"pause":"ask"}' })
    const result = { ...unknown, seq: checkpointFirst ? 4 : 3 }
    const events = checkpointFirst ? [call, checkpoint, result] : [call, result, checkpoint]
    expect(card(events)?.[0].question?.pending).toBe(false)
    const restored = card([...events, recovered])!
    expect(restored).toHaveLength(1)
    expect(restored[0]).toMatchObject({ seq: 2, turnId: "turn", question: { callId: "ask-original", pending: true } })
    expect(restored[0].question?.questions[0].prompt).toBe("Choose a path")
  })
  it("does not erase a real answer or rearm it on a duplicate call, recovery or unknown result", () => {
    const settled = card([call, answer, { ...unknown, seq: 7 }, { ...call, seq: 8 }, { ...recovered, seq: 9 }])!
    expect(settled).toHaveLength(1)
    expect(settled[0].question).toMatchObject({ pending: false, answers: { path: "First" } })
  })
  it("accepts a delayed real answer ahead of a recovery and ignores a delayed older unknown", () => {
    let state = reduceEvents(emptyTranscript(), [call, recovered])
    state = reduceEvent(state, unknown)
    expect(state.agents.manager.blocks[0].question?.pending).toBe(true)
    state = reduceEvent(state, { ...answer, seq: 4 })
    state = reduceEvent(state, { ...recovered, seq: 7 })
    expect(state.agents.manager.blocks[0].question).toMatchObject({ pending: false, answers: { path: "First" } })
  })
  it("can restore a tail page without fabricating another tool call and keeps repeated recovery idempotent", () => {
    const restored = card([recovered, recovered, { ...recovered, seq: 7 }, call])!
    expect(restored).toHaveLength(1)
    expect(restored[0].question).toMatchObject({ callId: "ask-original", pending: true })
  })
  it("does not use a duplicate call or an unverified recovery as authority", () => {
    expect(card([call, unknown, { ...call, seq: 7 }])?.[0].question?.pending).toBe(false)
    for (const extra of [{ agent_id: "worker" }, { tool_call_id: "" }, { turn_id: "" }, { text: "{}" }, { seq: 0 }]) {
      const settled = card([call, unknown, { ...recovered, ...extra }])!
      expect(settled).toHaveLength(1)
      expect(settled[0].question?.pending).toBe(false)
    }
    const external = reduceEvents(emptyTranscript(), [event("tool_call", 2, { text: "exec({})" }), { ...unknown, err: "outcome_unknown" }])
    expect(external.agents.manager.blocks[0].tool).toMatchObject({ pending: false, failed: true })
  })
})

it("never accepts a worker or another turn result as the manager human answer", () => {
  const state = reduceEvents(emptyTranscript(), [call,
    event("tool_call", 3, { agent_id: "worker", text: "exec({})" }),
    { ...answer, seq: 4, agent_id: "worker" },
    { ...answer, seq: 6, turn_id: "other" },
    { ...recovered, seq: 7, agent_id: "worker" },
  ])
  expect(state.agents.manager.blocks.find((b) => b.question)?.question).toMatchObject({ pending: true })
  expect(state.agents.manager.blocks.find((b) => b.question)?.question?.answers).toBeUndefined()
})

it("keeps separate original questions when the model reuses a call ID in a later turn", () => {
  const currentCall = { ...call, seq: 8, turn_id: "next" }
  const currentUnknown = { ...unknown, seq: 9, turn_id: "next" }
  const currentRecovery = { ...recovered, seq: 10, turn_id: "next" }
  let state = reduceEvents(emptyTranscript(), [call, answer, currentCall, currentUnknown, currentRecovery])
  const questions = () => state.agents.manager.blocks.filter((b) => b.question)
  expect(questions()).toHaveLength(2)
  expect(questions()[0]).toMatchObject({ turnId: "turn", question: { pending: false, answers: { path: "First" } } })
  expect(questions()[1]).toMatchObject({ turnId: "next", question: { pending: true, callId: "ask-original" } })
  state = reduceEvent(state, { ...answer, seq: 11, turn_id: "next", text: '{"answers":{"path":{"answers":["Second"]}}}' })
  expect(questions()[1].question).toMatchObject({ pending: false, answers: { path: "Second" } })
})
it("does not confirm partial, foreign-ID or error-decorated answers", () => {
  const questions = [...JSON.parse(args).questions, { ...JSON.parse(args).questions[0], id: "confirm" }]
  const two = JSON.stringify({ questions })
  for (const text of [
    '{"answers":{"path":{"answers":["First"]}}}',
    '{"answers":{"foreign":{"answers":["First"]}}}',
    '{"code":"outcome_unknown","answers":{"path":{"answers":["First"]},"confirm":{"answers":["Second"]}}}',
    '{"error":"interrupted","answers":{"path":{"answers":["First"]},"confirm":{"answers":["Second"]}}}',
  ]) {
    const question = card([{ ...call, text: `ask_user(${two})` }, { ...unknown, text }, { ...recovered, text: two }])![0].question
    expect(question?.pending).toBe(true)
    expect(question?.answers).toBeUndefined()
  }
})
