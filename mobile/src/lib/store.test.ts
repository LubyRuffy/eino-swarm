import { describe, expect, it } from "vitest"

import {
  clearLastThreadId,
  clearLink,
  hubHost,
  linkLabel,
  loadActiveFingerprint,
  loadLastThreadId,
  loadSavedLink,
  loadSavedLinks,
  removeLink,
  saveActiveFingerprint,
  saveHostLabel,
  saveLastThreadId,
  saveLink,
  type SavedLink,
} from "./store"

function link(fp: string, hubURL = "https://hub.example.test"): SavedLink {
  return {
    hubURL,
    ticket: "tick-" + fp,
    hostPub: "pub-" + fp,
    sessionID: "sid-" + fp,
    fingerprint: fp,
  }
}

describe("saved links", () => {
  it("remembers the id the phone last opened and forgets it on unlink", () => {
    expect(loadLastThreadId()).toBe("")
    saveLastThreadId("  t1  ")
    expect(loadLastThreadId()).toBe("t1")
    saveLastThreadId("")
    expect(loadLastThreadId()).toBe("t1")
    saveLink(link("fp"))
    clearLink()
    expect(loadLastThreadId()).toBe("")
  })

  it("clears an explicit last id without touching a missing link", () => {
    saveLastThreadId("t2")
    clearLastThreadId()
    expect(loadLastThreadId()).toBe("")
  })

  it("migrates a legacy single ticket into the host list", () => {
    localStorage.setItem(
      "zwai.remote.link",
      JSON.stringify({
        hubURL: "https://hub.example.test",
        ticket: "tick",
        hostPub: "pub",
        sessionID: "sid",
        fingerprint: "legacy",
      }),
    )
    const links = loadSavedLinks()
    expect(links).toHaveLength(1)
    expect(links[0].fingerprint).toBe("legacy")
    expect(localStorage.getItem("zwai.remote.link")).toBeNull()
    expect(loadSavedLink()?.fingerprint).toBe("legacy")
  })

  it("keeps a second PC instead of overwriting the first ticket", () => {
    saveLink(link("a", "https://office.example.test"))
    saveLink(link("b", "https://home.example.test"))
    const fps = loadSavedLinks().map((row) => row.fingerprint).sort()
    expect(fps).toEqual(["a", "b"])
    expect(loadActiveFingerprint()).toBe("b")
    saveActiveFingerprint("a")
    expect(loadSavedLink()?.fingerprint).toBe("a")
    saveLastThreadId("thread-a")
    saveActiveFingerprint("b")
    expect(loadLastThreadId()).toBe("")
    saveActiveFingerprint("a")
    expect(loadLastThreadId()).toBe("thread-a")
  })

  it("replaces the ticket when the same PC is scanned again", () => {
    saveLink(link("a", "https://hub.example.test"))
    saveLink({
      ...link("a", "https://hub.example.test"),
      ticket: "tick-a-new",
    })
    const links = loadSavedLinks()
    expect(links).toHaveLength(1)
    expect(links[0].ticket).toBe("tick-a-new")
    expect(loadActiveFingerprint()).toBe("a")
  })

  it("keeps a cached computer name when the same PC is scanned again", () => {
    saveLink({ ...link("a"), label: "desk-one" })
    saveLink(link("a"))
    expect(loadSavedLinks()[0]?.label).toBe("desk-one")
    expect(loadSavedLinks()[0]?.ticket).toBe("tick-a")
  })

  it("caches a computer name without stealing the active host", () => {
    saveLink(link("a"))
    saveLink(link("b"))
    saveActiveFingerprint("b")
    saveHostLabel("a", "  desk-one  ")
    expect(loadActiveFingerprint()).toBe("b")
    expect(loadSavedLinks().find((row) => row.fingerprint === "a")?.label).toBe("desk-one")
  })

  it("drops one host and keeps the other as active", () => {
    saveLink(link("a"))
    saveLink(link("b"))
    const rest = removeLink("b")
    expect(rest.map((row) => row.fingerprint)).toEqual(["a"])
    expect(loadActiveFingerprint()).toBe("a")
  })
})

describe("linkLabel", () => {
  it("uses the PC name and never the hub hostname", () => {
    expect(hubHost("wss://relay.example.test:2440/pairlink")).toBe("relay.example.test")
    const a = link("aaaa", "https://hub.example.test")
    const b = link("bbbb", "https://hub.example.test")
    expect(linkLabel(a, [a])).toBe("aaaa")
    expect(linkLabel(a, [a, b])).toBe("aaaa")
    expect(linkLabel({ ...a, label: "desk-one" }, [a, b])).toBe("desk-one")
    expect(linkLabel({ ...a, label: "desk-one" }, [{ ...b, label: "desk-one" }, a])).toBe(
      "desk-one · aaaa",
    )
    expect(linkLabel(a, [a])).not.toBe("hub.example.test")
    expect(linkLabel(a, [a])).not.toMatch(/MacBook Pro|iMac|Office/)
  })
})
