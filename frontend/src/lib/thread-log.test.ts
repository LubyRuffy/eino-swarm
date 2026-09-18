import { describe, expect, it } from "vitest"

import {
  extraRosterEvents,
  LOG_LIMIT_DEFAULT,
  LOG_LIMIT_MAX,
  LOG_LIMIT_MIN,
  historyNewestSeq,
  historyOldestSeq,
  logPageSize,
  mergeLogEvents,
  shouldLoadOlderHistory,
  type ThreadLog,
  uniqueStoredEvents,
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
    const page: ThreadLog = { events: [ev(3)], has_more: true, roster: [] }
    expect(JSON.stringify(page)).not.toMatch(/notes\.md|deadline/i)
  })
})

describe("roster sidecar", () => {
  function spawn(seq: number, id: string): SwarmEvent {
    return {
      thread_id: "th_1",
      turn_id: "tn_1",
      seq,
      kind: "spawned",
      agent_id: id,
      role: "worker",
      created_at: new Date().toISOString(),
    }
  }

  it("keeps spawned rows that are not on the page", () => {
    const extra = extraRosterEvents([spawn(2, "worker-1")], [ev(40), ev(41)])
    expect(extra.map((e) => e.seq)).toEqual([2])
    const merged = mergeLogEvents([spawn(2, "worker-1")], [ev(40), ev(41)])
    expect(merged.map((e) => e.seq)).toEqual([2, 40, 41])
  })

  it("does not duplicate a spawned row already on the page", () => {
    const page = [spawn(2, "worker-1"), ev(40)]
    expect(extraRosterEvents([spawn(2, "worker-1")], page)).toEqual([])
    expect(mergeLogEvents([spawn(2, "worker-1")], page).map((e) => e.seq)).toEqual([
      2, 40,
    ])
  })

  it("leaves the page cursor with the caller", () => {
    const page = [ev(40)]
    const extra = extraRosterEvents([spawn(2, "worker-1")], page)
    expect(historyOldestSeq(page)).toBe(40)
    expect(historyOldestSeq(extra)).toBe(2)
  })

  it("treats a missing sidecar as no extra rows", () => {
    expect(extraRosterEvents(undefined, [ev(40)])).toEqual([])
    expect(mergeLogEvents(undefined, [ev(40)]).map((e) => e.seq)).toEqual([40])
  })
})

describe("uniqueStoredEvents", () => {
  it("keeps the first row for a duplicated seq", () => {
    const first = ev(2)
    first.kind = "spawned"
    const second = ev(2)
    second.kind = "spawned"
    second.text = "duplicate"
    expect(uniqueStoredEvents([first, second, ev(40)]).map((e) => e.seq)).toEqual([2, 40])
    expect(uniqueStoredEvents([first, second])[0]?.text).toBeUndefined()
  })
})
