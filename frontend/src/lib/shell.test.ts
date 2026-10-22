import { afterEach, describe, expect, it, vi } from "vitest"

import { desktopShellFrom, openLinksNatively, startPresence, uniqueSurfaces } from "./shell"

describe("desktopShellFrom", () => {
  it("treats the desktop query and the native bridge as a window", () => {
    expect(desktopShellFrom("?shell=desktop", false)).toBe(true)
    expect(desktopShellFrom("", true)).toBe(true)
    expect(desktopShellFrom("?shell=web", false)).toBe(false)
    expect(desktopShellFrom("", false)).toBe(false)
  })
})

describe("openLinksNatively", () => {
  it("uses the system browser only in the desktop window", () => {
    expect(openLinksNatively(true, true)).toBe(true)
    expect(openLinksNatively(true, false)).toBe(false)
    expect(openLinksNatively(false, true)).toBe(false)
    expect(openLinksNatively(undefined, false)).toBe(false)
  })
})
describe("uniqueSurfaces", () => {
  it("keeps the first of each surface and drops blanks", () => {
    expect(
      uniqueSurfaces([
        { surface: "desktop" },
        { surface: "desktop" },
        { surface: " " },
        { surface: "tui" },
      ]),
    ).toEqual(["desktop", "tui"])
  })
})

describe("startPresence", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    window.history.replaceState(null, "", "/")
  })

  it("does nothing when the client cannot hold a connection", () => {
    expect(() => startPresence({}, "web")).not.toThrow()
  })

  it("holds the id the engine reserved", async () => {
    const holdPresence = vi.fn()
    startPresence(
      {
        presence: async () => ({ id: "pc_1" }),
        holdPresence,
      },
      "desktop",
    )
    await vi.waitFor(() => expect(holdPresence).toHaveBeenCalledWith("pc_1"))
  })

  it("leaves a launcher's desktop presence under native lifecycle ownership", async () => {
    const presence = vi.fn(async () => ({ id: "pc_new" }))
    const holdPresence = vi.fn()
    startPresence({ presence, holdPresence }, "desktop", "?presence=pc_0123456789abcdef")
    await Promise.resolve()
    expect(presence).not.toHaveBeenCalled()
    expect(holdPresence).not.toHaveBeenCalled()
  })

  it("does not replace the native hold when the client only implements holding", async () => {
    const holdPresence = vi.fn()
    startPresence({ holdPresence }, "desktop", "?presence=pc_0123456789abcdef")
    await Promise.resolve()
    expect(holdPresence).not.toHaveBeenCalled()
  })

  it("reads native presence ownership from the current window by default", async () => {
    window.history.replaceState(null, "", "/?shell=desktop&presence=pc_0123456789abcdef")
    const presence = vi.fn(async () => ({ id: "pc_new" }))
    const holdPresence = vi.fn()
    startPresence({ presence, holdPresence }, "desktop")
    await Promise.resolve()
    expect(presence).not.toHaveBeenCalled()
    expect(holdPresence).not.toHaveBeenCalled()
  })

  it("reserves an independent web client despite a desktop handoff query", async () => {
    const presence = vi.fn(async () => ({ id: "pc_web" }))
    const holdPresence = vi.fn()
    startPresence({ presence, holdPresence }, "web", "?presence=pc_0123456789abcdef")
    await vi.waitFor(() => expect(holdPresence).toHaveBeenCalledWith("pc_web"))
    expect(presence).toHaveBeenCalledWith("web")
    expect(holdPresence).toHaveBeenCalledTimes(1)
  })

  it.each([
    "",
    "?presence=",
    "?presence=pc_0123456789abcde",
    "?presence=pc_0123456789abcdef0",
    "?presence=pc_0123456789abcdeg",
    "?presence=pc_0123456789ABCDEF",
    "?presence=../pc_0123456789abcdef",
    "?presence=pc_0123456789abcdef%2Fother",
  ])("reserves a desktop client when no valid handoff is present (%s)", async (search) => {
    const presence = vi.fn(async () => ({ id: "pc_fresh" }))
    const holdPresence = vi.fn()
    startPresence({ presence, holdPresence }, "desktop", search)
    await vi.waitFor(() => expect(holdPresence).toHaveBeenCalledWith("pc_fresh"))
    expect(presence).toHaveBeenCalledWith("desktop")
    expect(holdPresence).toHaveBeenCalledTimes(1)
  })

  it("does not reserve or throw when a handoff client cannot hold", () => {
    const presence = vi.fn(async () => ({ id: "pc_new" }))
    expect(() => startPresence({ presence }, "desktop", "?presence=pc_0123456789abcdef")).not.toThrow()
    expect(presence).not.toHaveBeenCalled()
  })

  it("reserves normally without a browser window", async () => {
    const presence = vi.fn(async () => ({ id: "pc_ssr" }))
    const holdPresence = vi.fn()
    vi.stubGlobal("window", undefined)
    try {
      expect(() => startPresence({ presence, holdPresence }, "desktop")).not.toThrow()
    } finally {
      vi.unstubAllGlobals()
    }
    await vi.waitFor(() => expect(holdPresence).toHaveBeenCalledWith("pc_ssr"))
    expect(presence).toHaveBeenCalledWith("desktop")
  })

  it("keeps boot available without invoking a hold while native owns presence", async () => {
    const holdPresence = vi.fn(() => { throw new Error("presence unavailable") })
    expect(() => startPresence({ holdPresence }, "desktop", "?presence=pc_0123456789abcdef")).not.toThrow()
    await Promise.resolve()
    expect(holdPresence).not.toHaveBeenCalled()
  })
})
