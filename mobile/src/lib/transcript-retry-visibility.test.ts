import { expect, it } from "vitest"

import { applyEvent } from "./transcript"

// The PC's Earlier-page filter must use this same visible wire protocol.
it.each([
  "workers_uncollected", "schedule_claim_unverified", "context_overflow", "invalid_tool_json", "rate_limited",
  "model_unavailable", "model_response_invalid", "model_transport", "model_idle",
])("renders the %s retry protocol reason as a phone notice", (reason) => {
  const blocks = applyEvent([], {
    thread_id: "thread", seq: 1, kind: "model_retry", created_at: "2026-01-01T00:00:00Z",
    text: JSON.stringify({ reason }),
  })
  expect(blocks).toHaveLength(1)
  expect(blocks[0].kind).toBe("notice")
  expect(blocks[0].text).not.toContain(reason)
})

it.each([
  '{"reason":"future_reason"}', '{"attempt":1}', '{"reason":', '{"reason":5}', "null", "legacy retry envelope",
])("keeps an invisible retry envelope out of the phone transcript: %s", (text) => {
  expect(applyEvent([], {
    thread_id: "thread", seq: 1, kind: "model_retry", created_at: "2026-01-01T00:00:00Z", text,
  })).toEqual([])
})
