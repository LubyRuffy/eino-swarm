import { describe, expect, it, vi } from "vitest"

import { desktopShellFrom, startPresence, uniqueSurfaces } from "./shell"

describe("desktopShellFrom", () => {
  it("treats the desktop query and the native bridge as a window", () => {
    expect(desktopShellFrom("?shell=desktop", false)).toBe(true)
    expect(desktopShellFrom("", true)).toBe(true)
    expect(desktopShellFrom("?shell=web", false)).toBe(false)
    expect(desktopShellFrom("", false)).toBe(false)
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
})
