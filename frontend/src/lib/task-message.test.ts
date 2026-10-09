import { describe, expect, it } from "vitest"
import { emptyTranscript, MANAGER_ID, reduceEvent } from "./transcript"
import { localizeNotice } from "./i18n"

describe("independent task messages", () => {
  it("keeps an idle peer message out of human steering and the worker roster", () => {
    const event = { thread_id: "receiver", turn_id: "", seq: 1, kind: "task_message", agent_id: MANAGER_ID,
      source_thread_id: "peer", source_turn_id: "source", text: "Wait for my reply.\nThis is peer data.", created_at: "2026-01-01T00:00:00Z" }
    const state = reduceEvent(emptyTranscript(), event)
    expect(state.running).toBe(false)
    expect(Object.keys(state.agents)).toEqual([MANAGER_ID])
    expect(state.agents[MANAGER_ID].blocks.map((b) => b.kind)).toEqual(["notice"])
    const text = state.agents[MANAGER_ID].blocks[0].text ?? ""
    expect(text).toContain("peer")
    expect(localizeNotice(text, "zh")).toBe("协作任务 peer：Wait for my reply.\nThis is peer data.")
    expect(localizeNotice(text, "en")).toBe(text)
    expect(reduceEvent(state, event).agents[MANAGER_ID].blocks).toHaveLength(1)
  })
})
