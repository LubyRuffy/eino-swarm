import { describe, expect, it } from "vitest"

import { mergeThreadList, preferNamedTitles, setThreadRunning, askingThreadIds, upsertThread, threadListOverlay } from "./thread-title"
import type { Thread } from "./types"

function thread(id: string, title: string, title_auto: boolean): Thread {
  return {
    id,
    title,
    title_auto,
    project_id: "",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: "",
    last_active_at: "",
    running: false,
  }
}

describe("preferNamedTitles", () => {
  it("keeps a generated name when the list is still the placeholder", () => {
    const local = [thread("th_1", "Weekly status", false)]
    const incoming = [thread("th_1", "Investigate the overdue items…", true)]
    expect(preferNamedTitles(local, incoming)[0]?.title).toBe("Weekly status")
    expect(preferNamedTitles(local, incoming)[0]?.title_auto).toBe(false)
  })

  it("keeps a generated name when the list omits title_auto", () => {
    const local = [thread("th_1", "Weekly status", false)]
    const incoming = [{ ...thread("th_1", "Investigate the overdue items…", true) }]
    delete incoming[0].title_auto
    expect(preferNamedTitles(local, incoming)[0]?.title).toBe("Weekly status")
    expect(preferNamedTitles(local, incoming)[0]?.title_auto).toBe(false)
  })

  it("takes the list once the namer has landed there", () => {
    const local = [thread("th_1", "Weekly status", false)]
    const incoming = [thread("th_1", "Ops report", false)]
    expect(preferNamedTitles(local, incoming)[0]?.title).toBe("Ops report")
  })
})

describe("upsertThread", () => {
  it("inserts a minted conversation that is not already in the list", () => {
    const existing = [thread("th_old", "earlier", true)]
    const minted = thread("th_new", "periodic check", false)
    const next = upsertThread(existing, minted)
    expect(next[0]?.id).toBe("th_new")
    expect(next[0]?.title).toBe("periodic check")
    expect(next.map((t) => t.id)).toEqual(["th_new", "th_old"])
  })

  it("keeps a generated name when replacing an existing row", () => {
    const local = [thread("th_1", "Weekly status", false)]
    const incoming = thread("th_1", "Investigate the overdue items…", true)
    expect(upsertThread(local, incoming)[0]?.title).toBe("Weekly status")
  })
})

describe("setThreadRunning", () => {
  it("stamps one conversation without rewriting the rest", () => {
    const listed = [thread("th_1", "A", true), thread("th_2", "B", true)]
    const next = setThreadRunning(listed, "th_1", true)
    expect(next[0]?.running).toBe(true)
    expect(next[1]?.running).toBe(false)
    expect(setThreadRunning(next, "th_1", true)).toBe(next)
  })

  it("stamps a blocked ask so the sidebar is not a working pulse", () => {
    const listed = [thread("th_1", "A", true)]
    const next = setThreadRunning(listed, "th_1", true, true)
    expect(next[0]).toMatchObject({ running: true, awaiting_answer: true })
    expect(setThreadRunning(next, "th_1", true, true)).toBe(next)
    expect(setThreadRunning(next, "th_1", false)?.[0]?.awaiting_answer).toBe(false)
  })
})

describe("askingThreadIds", () => {
  it("unions the listing with the open conversation's live status", () => {
    const listed = [
      { ...thread("th_1", "A", true), awaiting_answer: true },
      thread("th_2", "B", true),
    ]
    expect([...askingThreadIds(listed, "th_2", true)].sort()).toEqual([
      "th_1",
      "th_2",
    ])
  })
})

describe("mergeThreadList", () => {
  it("lights a conversation the listing says is running, even if we never opened it", () => {
    const local = [thread("th_1", "A", true), thread("th_2", "B", true)]
    const incoming = [
      thread("th_1", "A", true),
      setThreadRunning([thread("th_2", "B", true)], "th_2", true)[0]!,
    ]
    expect(mergeThreadList(local, incoming)[1]?.running).toBe(true)
  })

  it("idles a background conversation the listing says finished", () => {
    const local = [setThreadRunning([thread("th_1", "A", true)], "th_1", true)[0]!]
    const incoming = [thread("th_1", "A", true)]
    expect(mergeThreadList(local, incoming)[0]?.running).toBe(false)
  })

  it("keeps the open conversation lit when a listing fetch raced Enter", () => {
    const local = [setThreadRunning([thread("th_1", "A", true)], "th_1", true)[0]!]
    const incoming = [thread("th_1", "A", true)]
    expect(
      mergeThreadList(local, incoming, { id: "th_1", running: true })[0]?.running,
    ).toBe(true)
  })

  it("does not relight the open conversation when a listing fetch raced done", () => {
    const local = [thread("th_1", "A", true)]
    const incoming = [setThreadRunning([thread("th_1", "A", true)], "th_1", true)[0]!]
    expect(
      mergeThreadList(local, incoming, { id: "th_1", running: false })[0]?.running,
    ).toBe(false)
  })

  it("keeps a blocked-ask overlay on the open conversation", () => {
    const local = [setThreadRunning([thread("th_1", "A", true)], "th_1", true, true)[0]!]
    const incoming = [setThreadRunning([thread("th_1", "A", true)], "th_1", true)[0]!]
    expect(
      mergeThreadList(local, incoming, {
        id: "th_1",
        running: true,
        awaitingAnswer: true,
      })[0]?.awaiting_answer,
    ).toBe(true)
  })

  it("lets setThreadRunning(false) stay idle on the next listing", () => {
    const local = [thread("th_1", "A", true)]
    const incoming = [thread("th_1", "A", true)]
    expect(mergeThreadList(local, incoming)[0]?.running).toBe(false)
  })

  it("keeps a parked wait on the open conversation when the listing is still idle", () => {
    const local = [thread("th_1", "A", true)]
    const incoming = [thread("th_1", "A", true)]
    expect(
      mergeThreadList(local, incoming, {
        id: "th_1",
        running: false,
        waiting: true,
      })[0]?.waiting,
    ).toBe(true)
  })

  it("does not let a live overlay idle a listing wait on the open conversation", () => {
    const incoming = [{ ...thread("th_1", "A", true), waiting: true }]
    expect(
      mergeThreadList([], incoming, {
        id: "th_1",
        running: false,
        waiting: false,
      })[0]?.waiting,
    ).toBe(true)
  })
})

describe("threadListOverlay", () => {
  it("stamps the open conversation and stays out of the way when none is open", () => {
    expect(threadListOverlay(undefined, { running: true })).toBeUndefined()
    expect(threadListOverlay("th_1", { running: true, awaiting_answer: true })).toEqual({
      id: "th_1",
      running: true,
      awaitingAnswer: true,
      waiting: false,
    })
    expect(threadListOverlay("th_1", { running: false, awaiting_answer: true })).toEqual({
      id: "th_1",
      running: false,
      awaitingAnswer: false,
      waiting: false,
    })
    expect(threadListOverlay("th_1", { running: false, waiting: true })).toEqual({
      id: "th_1",
      running: false,
      awaitingAnswer: false,
      waiting: true,
    })
  })
})
