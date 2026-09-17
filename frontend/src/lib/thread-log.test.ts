import { describe, expect, it } from "vitest"

import {
  LOG_LIMIT_DEFAULT,
  LOG_LIMIT_MAX,
  LOG_LIMIT_MIN,
  historyNewestSeq,
  historyOldestSeq,
  logPageSize,
  shouldLoadOlderHistory,
  type ThreadLog,
} from "./thread-log"
import type { SwarmEvent } from "./types"

function ev(seq: number): SwarmEvent {
  return {
    thread_id: "th_1",
    turn_id: "tn_1",
    seq,
    kind: "user_message",
    agent_id: "manager",
    created_at: new Date().toISOString(),
  }
}

describe("logPageSize", () => {
  it("uses a default when the scroller has not laid out yet", () => {
    expect(logPageSize(0)).toBe(LOG_LIMIT_DEFAULT)
  })

  it("sizes the page to the viewport height", () => {
    expect(logPageSize(400)).toBeGreaterThanOrEqual(LOG_LIMIT_MIN)
    expect(logPageSize(400)).toBeLessThan(LOG_LIMIT_DEFAULT)
    expect(logPageSize(20_000)).toBe(LOG_LIMIT_MAX)
  })

  it("does not take its size from a sample conversation", () => {
    expect(JSON.stringify(logPageSize(800))).not.toMatch(/notes\.md|deadline/i)
  })
})

describe("history seq cursors", () => {
  it("skips streamed deltas when finding the resume point", () => {
    const events = [ev(0), ev(4), ev(5)]
    expect(historyOldestSeq(events)).toBe(4)
    expect(historyNewestSeq(events)).toBe(5)
    expect(historyOldestSeq([])).toBe(0)
    expect(historyNewestSeq([])).toBe(0)
  })
})

describe("shouldLoadOlderHistory", () => {
  it("loads when the tail does not fill the pane", () => {
    expect(shouldLoadOlderHistory(true, false, 0, 200, 400)).toBe(true)
  })

  it("loads when the reader reaches the top", () => {
    expect(shouldLoadOlderHistory(true, false, 12, 2000, 400)).toBe(true)
    expect(shouldLoadOlderHistory(true, false, 80, 2000, 400)).toBe(false)
  })

  it("does not fetch while a page is already in flight or the log is complete", () => {
    expect(shouldLoadOlderHistory(true, true, 0, 200, 400)).toBe(false)
    expect(shouldLoadOlderHistory(false, false, 0, 200, 400)).toBe(false)
  })
})

describe("ThreadLog", () => {
  it("is a page plus a cursor, not a sample payload", () => {
    const page: ThreadLog = { events: [ev(3)], has_more: true }
    expect(JSON.stringify(page)).not.toMatch(/notes\.md|deadline/i)
  })
})
