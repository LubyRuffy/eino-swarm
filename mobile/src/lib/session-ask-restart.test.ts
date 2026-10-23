import { describe, expect, it } from "vitest"
import { applyEvent, pendingAsk, type CompactBlock } from "./transcript"
import { applyPush, openView, prependOlder } from "./session"
import { OpEvent, OpReady, type RemoteEvent, type ThreadDetail } from "./rpc"

const args = JSON.stringify({ questions: [{ id: "path", prompt: "Choose a path", options: [{ id: "one", label: "First" }, { id: "two", label: "Second" }] }] })
function event(kind: string, seq: number, extra: Partial<RemoteEvent> = {}): RemoteEvent {
  return { kind, seq, thread_id: "thread", turn_id: "turn", agent_id: "manager", tool_call_id: "ask-original", created_at: "2026-10-23T12:00:00Z", text: "", ...extra }
}
const call = event("tool_call", 2, { text: `ask_user(${args})` })
const unknown = event("tool_result", 3, { text: '{"outcome":"unknown","error":"outcome_unknown"}' })
const recovered = event("ask_recovered", 5, { text: args })
const answer = event("tool_result", 6, { text: '{"answers":{"path":{"answers":["First"]}}}' })
const fold = (events: RemoteEvent[]) => events.reduce((blocks, ev) => applyEvent(blocks, ev), [] as CompactBlock[])
const detail: ThreadDetail = { id: "thread", title: "Task", running: { thread_id: "thread", title: "Task", turn_id: "turn", ask_user: true } }
const ready = (events: RemoteEvent[]) => applyPush(openView(detail), { v: 1, id: "", ok: true, op: OpReady, thread_id: "thread", seq: Math.max(...events.map((e) => e.seq)), events, status: { running: true, turn_id: "turn", awaiting_answer: true } })
const push = (ev: RemoteEvent) => ({ v: 1 as const, id: "", ok: true, op: OpEvent, thread_id: "thread", seq: ev.seq, event: ev })

describe("phone restart preserves the original human question", () => {
  it.each([false, true])("requires explicit recovery, checkpoint first=%s", (checkpointFirst) => {
    const checkpoint = event("engine_restart_checkpoint", checkpointFirst ? 3 : 4, { text: '{"pause":"ask"}' })
    const result = { ...unknown, seq: checkpointFirst ? 4 : 3 }
    const events = checkpointFirst ? [call, checkpoint, result] : [call, result, checkpoint]
    expect(pendingAsk(fold(events))).toBeUndefined()
    const blocks = fold([...events, recovered])
    expect(blocks.filter((b) => b.kind === "question")).toHaveLength(1)
    expect(pendingAsk(blocks)).toMatchObject({ callId: "ask-original", turnId: "turn", pending: true })
  })
  it("retains the accepted answer and does not reopen it on late unknown/call/recovery", () => {
    const blocks = fold([call, answer, { ...unknown, seq: 7 }, { ...call, seq: 8 }, { ...recovered, seq: 9 }])
    expect(blocks.filter((b) => b.kind === "question")).toHaveLength(1)
    expect(blocks.find((b) => b.kind === "question")).toMatchObject({ pending: false, answers: { path: "First" } })
    expect(pendingAsk(blocks)).toBeUndefined()
  })
  it("keeps both live and ready replay status tied to the actual pending question", () => {
    let view = ready([call, unknown])
    view = applyPush(view, push(recovered))
    expect(pendingAsk(view.blocks)?.callId).toBe("ask-original")
    expect(view.detail?.running?.ask_user).toBe(true)
    view = applyPush(view, push(answer))
    view = applyPush(view, push({ ...call, seq: 8 }))
    expect(view.detail?.running?.ask_user).toBe(false)
    expect(pendingAsk(view.blocks)).toBeUndefined()
    const replay = ready(view.events)
    expect(replay.blocks).toEqual(view.blocks)
    expect(replay.detail?.running?.ask_user).toBe(false)
  })
  it("reconciles a late older unknown and a real answered page without reopening or duplicating cards", () => {
    let view = ready([recovered])
    view = applyPush(view, push(unknown))
    expect(pendingAsk(view.blocks)?.callId).toBe("ask-original")
    view = prependOlder(view, [{ ...call, seq: 1 }, { ...answer, seq: 2 }], false)
    expect(view.blocks.filter((b) => b.kind === "question")).toHaveLength(1)
    expect(pendingAsk(view.blocks)).toBeUndefined()
    expect(view.detail?.running?.ask_user).toBe(false)
  })
  it("does not restore a generic unknown tool, wrong owner/turn, malformed payload or duplicate call", () => {
    expect(pendingAsk(fold([call, unknown, { ...call, seq: 8 }]))).toBeUndefined()
    for (const extra of [{ agent_id: "worker" }, { tool_call_id: "" }, { turn_id: "" }, { text: "{}" }, { seq: 0 }]) {
      expect(pendingAsk(fold([call, unknown, { ...recovered, ...extra }]))).toBeUndefined()
    }
    const external = fold([event("tool_call", 2, { text: "exec({})" }), { ...unknown, err: "outcome_unknown" }])
    expect(external[0]).toMatchObject({ kind: "tool", pending: false, failed: true })
  })
  it("lets a late older accepted answer clear live asking status without a watch snapshot", () => {
    let view = ready([call, unknown, recovered])
    view = applyPush(view, push({ ...answer, seq: 4 }))
    expect(pendingAsk(view.blocks)).toBeUndefined()
    expect(view.detail?.running?.ask_user).toBe(false)
  })
})

it("keeps manager asking independent of worker call-ID collisions and another turn's result", () => {
  let view = ready([call])
  for (const ev of [event("tool_call", 3, { agent_id: "worker", text: "exec({})" }),
    { ...answer, seq: 4, agent_id: "worker" }, { ...answer, seq: 6, turn_id: "other" },
    { ...recovered, seq: 7, agent_id: "worker" }]) view = applyPush(view, push(ev))
  expect(pendingAsk(view.blocks)?.callId).toBe("ask-original")
  expect(view.detail?.running?.ask_user).toBe(true)
})

it("does not let a worker tool with ask-shaped arguments set manager asking status", () => {
  const d = { ...detail, running: { ...detail.running!, ask_user: false } }
  let view = applyPush(openView(d), { v: 1, id: "", ok: true, op: OpReady, thread_id: "thread", seq: 0, status: { running: true, turn_id: "turn", awaiting_answer: false } })
  view = applyPush(view, push({ ...call, agent_id: "worker" }))
  expect(view.detail?.running?.ask_user).toBe(false)
})

it("keeps separate original questions when the model reuses a call ID in a later turn", () => {
  const old = { ...call, turn_id: "old" }
  const accepted = { ...answer, seq: 3, turn_id: "old" }
  const current = { ...call, seq: 4 }
  const failed = { ...unknown, seq: 5 }
  const restored = { ...recovered, seq: 6 }
  let view = ready([old, accepted, current, failed, restored])
  expect(view.blocks.filter((b) => b.kind === "question")).toHaveLength(2)
  expect(pendingAsk(view.blocks)).toMatchObject({ turnId: "turn", callId: "ask-original" })
  view = applyPush(view, push({ ...answer, seq: 7, text: '{"answers":{"path":{"answers":["Second"]}}}' }))
  expect(view.blocks.filter((b) => b.kind === "question").map((b) => b.answers)).toEqual([{ path: "First" }, { path: "Second" }])
  expect(view.detail?.running?.ask_user).toBe(false)
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
    const blocks = fold([{ ...call, text: `ask_user(${two})` }, { ...unknown, text }, { ...recovered, text: two }])
    expect(pendingAsk(blocks)).toMatchObject({ callId: "ask-original" })
    expect(pendingAsk(blocks)?.answers).toBeUndefined()
  }
})
