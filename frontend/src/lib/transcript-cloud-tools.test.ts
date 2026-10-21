import { describe, expect, it } from "vitest"

import { MANAGER_ID, emptyTranscript, reduceEvent, reduceEvents } from "./transcript"
import type { SwarmEvent } from "./types"

describe("provider-executed tool events", () => {
  it("retains quiet scheduled cloud receipts for Trace, including late receipts", () => {
    const event: SwarmEvent = { thread_id: "th-1", turn_id: "tn-1", seq: 1, kind: "schedule_fired", agent_id: MANAGER_ID, created_at: "2026-01-01T00:00:00Z" }
    const metadata = { provider_id: "endpoint-a", type: "file_search_call", status: "completed" }
    const beforeReport = reduceEvents(emptyTranscript(), [event, { ...event, seq: 2, kind: "builtin_tool", text: JSON.stringify(metadata) }])
    const state = reduceEvents(beforeReport, [
      { ...event, seq: 3, kind: "schedule_report", text: '{"quiet":true}' },
      { ...event, seq: 4, kind: "builtin_tool", text: JSON.stringify({ ...metadata, id: "late" }) },
    ])
    const rows = state.agents[MANAGER_ID].blocks
    expect(rows).toHaveLength(2)
    expect(rows.every((row) => row.quiet && row.cloudTool && row.detail)).toBe(true)
    expect(rows[0].cloudTool).toEqual(metadata)
    expect(rows[1].cloudTool?.id).toBe("late")
    expect(beforeReport.agents[MANAGER_ID].blocks[1].quiet).toBeUndefined()
  })
  it("keeps cloud tools distinct from local calls and never invents a worker", () => {
    const metadata = { provider_id: "endpoint-a", model: "model-a", id: "cloud-1", type: "file_search_call", status: "completed", action: { query: "topic" } }
    const event: SwarmEvent = { thread_id: "th-1", turn_id: "tn-1", seq: 1, kind: "builtin_tool", agent_id: "cloud", text: JSON.stringify(metadata), created_at: "2026-01-01T00:00:00Z" }
    const state = reduceEvent(emptyTranscript(), event)
    expect(state.agentOrder).toEqual([MANAGER_ID])
    const [row] = state.agents[MANAGER_ID].blocks
    expect(row.kind).toBe("notice")
    expect(row.text).toContain("Cloud tool")
    expect(row.text).toContain("file_search_call")
    expect(row.text).toContain("completed")
    expect(row.cloudTool).toEqual(metadata)
    expect(JSON.parse(row.detail ?? "")).toEqual(metadata)
    expect(row.tool).toBeUndefined()
    expect(row.spawn).toBeUndefined()
    expect(state.lastSeq).toBe(1)
  })

  it("retains malformed cloud event data for troubleshooting", () => {
    const state = reduceEvent(emptyTranscript(), { thread_id: "th-1", turn_id: "tn-1", seq: 2, kind: "builtin_tool", agent_id: "", text: "unparseable", created_at: "2026-01-01T00:00:00Z" })
    const [row] = state.agents[MANAGER_ID].blocks
    expect(row.kind).toBe("notice")
    expect(row.detail).toBe("unparseable")
    expect(row.cloudTool).toEqual({})
  })
})
