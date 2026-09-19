import { describe, expect, it } from "vitest"

import { mergeThreadList, preferNamedTitles, setThreadRunning, upsertThread } from "./thread-title"
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
})

describe("mergeThreadList", () => {
  it("keeps a live overlay when the listing still says idle", () => {
    const local = [setThreadRunning([thread("th_1", "A", true)], "th_1", true)[0]!]
    const incoming = [thread("th_1", "A", true)]
    expect(mergeThreadList(local, incoming)[0]?.running).toBe(true)
  })

  it("lets setThreadRunning(false) stay idle on the next listing", () => {
    const local = [thread("th_1", "A", true)]
    const incoming = [thread("th_1", "A", true)]
    expect(mergeThreadList(local, incoming)[0]?.running).toBe(false)
  })
})
