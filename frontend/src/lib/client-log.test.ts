import { describe, expect, it } from "vitest"

import { applyClientPage, type ClientLog } from "./client-log"

const user = { role: "user", text: "the request", at: 0 }
const earlier = { role: "assistant", text: "earlier reply", at: 10 }
const latest = { role: "assistant", text: "latest reply", at: 40 }
const synced = { role: "assistant", text: "synced reply", at: 80 }

describe("applyClientPage", () => {
  it("keeps an already loaded page when the live tail grows", () => {
    const opened = applyClientPage(
      null,
      { entries: [user, latest], older: true, before: 40 },
      "tail",
    )
    const expanded = applyClientPage(
      opened,
      { entries: [user, earlier], older: false, before: 10 },
      "older",
    )
    const polled = applyClientPage(
      expanded,
      { entries: [user, latest, synced], older: true, before: 40 },
      "tail",
    )
    expect(polled.body.map((entry) => entry.text)).toEqual([
      "earlier reply",
      "latest reply",
      "synced reply",
    ])
    expect(polled.opening.map((entry) => entry.text)).toEqual(["the request"])
    expect(polled.older).toBe(false)
    expect(polled.before).toBe(10)
    expect(polled.expanded).toBe(true)
  })

  it("replaces the tail until the reader asks for an earlier page", () => {
    const first = applyClientPage(
      null,
      { entries: [user, latest], older: true, before: 40 },
      "tail",
    )
    const next = applyClientPage(
      first,
      { entries: [user, synced], older: true, before: 80 },
      "tail",
    )
    expect(next.body.map((entry) => entry.text)).toEqual(["synced reply"])
    expect(next.before).toBe(80)
    expect(next.expanded).toBe(false)
  })

  it("updates a line that is still being written at the same offset", () => {
    const opened: ClientLog = applyClientPage(
      null,
      { entries: [user, { ...latest, text: "lat" }], older: false },
      "tail",
    )
    const grown = applyClientPage(
      { ...opened, expanded: true },
      { entries: [user, latest], older: false },
      "tail",
    )
    expect(grown.body.map((entry) => entry.text)).toEqual(["latest reply"])
  })
})
