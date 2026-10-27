import { describe, expect, it } from "vitest"
import { collectionDecision, collectionEvent, stoppedCollectionEvents } from "@/test/worker-collection-events"
import { emptyTranscript, liveWorkers, reduceEvent, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"

function notices(state: ReturnType<typeof emptyTranscript>) {
  return Object.values(state.agents).flatMap((agent) => agent.blocks.filter((block) => block.kind === "notice"))
}

describe("worker collection Stop decisions", () => {
  it("settles only the matching wait without treating its historical running snapshot as a lifecycle fact", () => {
    const events = stoppedCollectionEvents()
    const before = reduceEvents(emptyTranscript(), events.slice(0, 5))
    const state = reduceEvent(before, events[5])
    expect(state.agents.manager.blocks.find((block) => block.confirm)?.confirm).toMatchObject({ pending: false, continued: false, waitId: "wait-1", declinedSeq: 14 })
    expect(notices(state).some((block) => block.text === collectionDecision)).toBe(false)
    expect(state.agents["worker-1"]).toEqual(before.agents["worker-1"])
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.running).toBe(true)
    expect(state.lastSeq).toBe(14)
    expect(events[5].text).toBe(collectionDecision)
  })

  it.each(["before-cleanup", "after-cleanup", "after-terminal"] as const)(
    "keeps live and cold Stop replay identical when the real finish is %s", (order) => {
      const events = stoppedCollectionEvents()
      const finish = events.pop()!
      if (order === "before-cleanup") events.splice(6, 0, { ...finish, seq: 15 })
      else if (order === "after-cleanup") {
        events[7] = { ...events[7], seq: 18 }
        events.splice(7, 0, { ...finish, seq: 17 })
      }
      else events.push(finish)
      const live = reduceEvents(reduceEvents(emptyTranscript(), events.slice(0, 6)), events.slice(6))
      const cold = reduceEvents(emptyTranscript(), events)
      expect(cold).toEqual(live)
      expect(notices(cold).some((block) => block.text === collectionDecision)).toBe(false)
      expect(cold.turns[0].status).toBe("cancelled")
      expect(cold.agents["worker-1"]).toMatchObject({ status: "cancelled", activity: "stopped" })
      expect(cold.agents["worker-1"].error).toBeUndefined()
      expect(cold.agents["worker-1"].blocks.every((block) => !block.streaming && !block.tool?.pending)).toBe(true)
      expect(cold.running).toBe(false)
      expect(liveWorkers(cold)).toBe(0)
    },
  )

  it.each([
    { name: "a different wait", text: '{"wait_id":"old-wait"}' },
    { name: "a different turn", turn_id: "old-turn", text: collectionDecision },
    { name: "an older receipt", seq: 12, text: collectionDecision },
    { name: "malformed metadata", text: "{" },
    { name: "an empty wait", text: '{"wait_id":""}' },
    { name: "a worker actor", agent_id: "worker-1", text: collectionDecision },
    { name: "a worker role", role: "verifier", text: collectionDecision },
  ])("hides $name without settling the current wait", ({ name: _name, ...extra }) => {
    const before = reduceEvents(emptyTranscript(), stoppedCollectionEvents().slice(0, 5))
    const receipt = collectionEvent(14, "workers_collection_declined", extra)
    const state = reduceEvent(before, receipt)
    expect(state.agents.manager.blocks).toEqual(before.agents.manager.blocks)
    expect(state.agents["worker-1"]).toEqual(before.agents["worker-1"])
    expect(state.agents.manager.blocks.find((block) => block.confirm)?.confirm?.pending).toBe(true)
  })

  it("does not create a chat agent from a metadata-only tail", () => {
    const receipt = collectionEvent(14, "workers_collection_declined", { text: collectionDecision })
    const state = reduceEvent(emptyTranscript(), receipt)
    expect(state.agentOrder).toEqual([])
    expect(state.agents).toEqual({})
    expect(state.lastSeq).toBe(14)
  })

  it("does not settle an older still-pending wait after a different wait becomes current", () => {
    const events = stoppedCollectionEvents().slice(0, 5)
    const newer = { ...events[4], seq: 15, text: events[4].text!.replace("wait-1", "wait-2") }
    const before = reduceEvents(emptyTranscript(), [...events, newer])
    const state = reduceEvent(before, collectionEvent(16, "workers_collection_declined", { text: collectionDecision }))
    expect(state.agents.manager.blocks).toEqual(before.agents.manager.blocks)
    expect(state.agents.manager.blocks.filter((block) => block.confirm?.pending)).toHaveLength(2)
  })

  it("clears the prior decision projection when a newer pause updates the same wait", () => {
    const events = stoppedCollectionEvents().slice(0, 6)
    const state = reduceEvents(emptyTranscript(), [...events, { ...events[4], seq: 15 }])
    const pause = state.agents.manager.blocks.find((block) => block.confirm)?.confirm
    expect(pause).toMatchObject({ pending: true, waitId: "wait-1", sourceSeq: 15 })
    expect(pause?.declinedSeq).toBeUndefined()
  })

  it("does not reopen a stopped wait or worker when the decision arrives after the terminal", () => {
    const events = stoppedCollectionEvents()
    const receipt = events.splice(5, 1)[0]
    const before = reduceEvents(emptyTranscript(), events)
    const state = reduceEvent(before, { ...receipt, seq: 20 })
    expect(state.agents).toEqual(before.agents)
    expect(state.turns).toEqual(before.turns)
    expect(state.running).toBe(false)
    expect(liveWorkers(state)).toBe(0)
  })

  it("keeps genuinely unknown visible events in the fallback transcript", () => {
    const event: SwarmEvent = collectionEvent(20, "future_user_notice", { text: "A visible future notice." })
    const state = reduceEvent(emptyTranscript(), event)
    expect(notices(state)).toContainEqual(expect.objectContaining({ text: event.text }))
  })

  it.each(["user_message", "agent_message", "tool_result", "future_user_notice"])(
    "preserves identical JSON when it is a legitimate %s body", (kind) => {
      const event = collectionEvent(20, kind, { text: collectionDecision })
      const state = reduceEvent(emptyTranscript(), event)
      const contents = state.agents.manager.blocks.flatMap((block) => [block.text, block.tool?.result])
      expect(contents).toContain(collectionDecision)
    },
  )
})
