import { describe, expect, it } from "vitest"

import { localizeNotice } from "./i18n"
import { KINDS } from "./stream"
import { MANAGER_ID, emptyTranscript, reduceEvent, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"

function backoff(text: string, agentId = "manager", seq = 2): SwarmEvent {
  return {
    thread_id: "th_1", turn_id: "tn_1", seq, kind: "model_backoff",
    agent_id: agentId, text, created_at: "2026-01-01T00:00:01.000Z",
  }
}

describe("model request backoff", () => {
  it("subscribes to the stored backoff event while the turn is running", () => {
    expect(KINDS).toContain("model_backoff")
  })

  it.each([5, 10, 20, 30])("shows a %i second wait as a notice instead of JSON", (seconds) => {
    const event = backoff(JSON.stringify({ attempt: 2, wait_ms: seconds * 1000 }))
    const state = reduceEvent(emptyTranscript(), event)
    expect(state.agentOrder).toEqual([MANAGER_ID])
    expect(state.agents[MANAGER_ID].blocks).toEqual([
      expect.objectContaining({ kind: "notice", text: `Waiting ${seconds}s before retrying the model request.`, turnId: "tn_1", seq: 2 }),
    ])
    expect(JSON.stringify(state.agents[MANAGER_ID].blocks)).not.toContain("wait_ms")
    expect(localizeNotice(state.agents[MANAGER_ID].blocks[0].text, "zh"))
      .toBe(`等待 ${seconds} 秒后重试模型请求。`)
    expect(localizeNotice(state.agents[MANAGER_ID].blocks[0].text, "en"))
      .toBe(`Waiting ${seconds}s before retrying the model request.`)
  })

  // A title/review model can also wait. A transport notice never proves a
  // worker was spawned, but the Full log still needs the actual caller id.
  it("keeps the caller identity without creating a worker or changing turn state", () => {
    const user = { ...backoff("Perform the check", "manager", 1), kind: "user_message" }
    const state = reduceEvents(emptyTranscript(), [user, backoff('{"attempt":1,"wait_ms":5000}', "reviewer")])
    expect(state.agentOrder).toEqual([MANAGER_ID])
    expect(state.agents.reviewer).toBeUndefined()
    expect(state.agents[MANAGER_ID].blocks.at(-1)).toMatchObject({ kind: "notice", agentId: "reviewer", turnId: "tn_1" })
    expect(state.turns).toEqual([expect.objectContaining({ id: "tn_1", status: "running", agentIds: [] })])
    expect(state.running).toBe(true)
    expect(state.lastSeq).toBe(2)
  })

  it("rounds a remaining fractional second up instead of promising an early retry", () => {
    const state = reduceEvent(emptyTranscript(), backoff('{"attempt":1,"wait_ms":4999}'))
    expect(state.agents[MANAGER_ID].blocks[0].text).toBe("Waiting 5s before retrying the model request.")
  })

  it.each(["", "broken", "null", "{}", '{"wait_ms":"5000"}', '{"wait_ms":0}', '{"wait_ms":-1}', '{"wait_ms":1e400}'])
    ("keeps malformed payload %j out of the chat", (text) => {
      const state = reduceEvent(emptyTranscript(), backoff(text))
      expect(state.agents[MANAGER_ID].blocks[0].text).toBe("Waiting before retrying the model request.")
      expect(localizeNotice(state.agents[MANAGER_ID].blocks[0].text, "zh"))
        .toBe("等待后重试模型请求。")
    })
})
