import { afterEach, describe, expect, it } from "vitest"

import {
  SIDEBAR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_MAX,
  SIDEBAR_WIDTH_MIN,
  SIDEBAR_WIDTH_VAR,
  applySidebarWidth,
  clampSidebarWidth,
  hydrateSidebarWidth,
  paintSidebarWidth,
  readSidebarWidth,
} from "./sidebar-width"

afterEach(() => {
  localStorage.clear()
  document.documentElement.style.removeProperty(SIDEBAR_WIDTH_VAR)
})

describe("sidebar width", () => {
  it("defaults when nothing is stored", () => {
    expect(readSidebarWidth()).toBe(SIDEBAR_WIDTH_DEFAULT)
  })

  it("rejects garbage and non-positive values instead of shrinking the list to nothing", () => {
    localStorage.setItem("zwai.sidebar.width", "nope")
    expect(readSidebarWidth()).toBe(SIDEBAR_WIDTH_DEFAULT)
    localStorage.setItem("zwai.sidebar.width", "0")
    expect(readSidebarWidth()).toBe(SIDEBAR_WIDTH_DEFAULT)
    localStorage.setItem("zwai.sidebar.width", "-12")
    expect(readSidebarWidth()).toBe(SIDEBAR_WIDTH_DEFAULT)
  })

  it("clamps a remembered width into the usable range", () => {
    expect(clampSidebarWidth(50)).toBe(SIDEBAR_WIDTH_MIN)
    expect(clampSidebarWidth(9999)).toBe(SIDEBAR_WIDTH_MAX)
    localStorage.setItem("zwai.sidebar.width", "80")
    expect(readSidebarWidth()).toBe(SIDEBAR_WIDTH_MIN)
    localStorage.setItem("zwai.sidebar.width", "800")
    expect(readSidebarWidth()).toBe(SIDEBAR_WIDTH_MAX)
  })

  it("paints the CSS variable on hydrate so the title bar can match before the list mounts", () => {
    localStorage.setItem("zwai.sidebar.width", "320")
    expect(hydrateSidebarWidth()).toBe(320)
    expect(document.documentElement.style.getPropertyValue(SIDEBAR_WIDTH_VAR)).toBe(
      "320px",
    )
  })

  it("remembers a drag and paints it", () => {
    expect(applySidebarWidth(300.4)).toBe(300)
    expect(localStorage.getItem("zwai.sidebar.width")).toBe("300")
    expect(document.documentElement.style.getPropertyValue(SIDEBAR_WIDTH_VAR)).toBe(
      "300px",
    )
    expect(readSidebarWidth()).toBe(300)
  })

  it("paints a live drag without writing storage", () => {
    paintSidebarWidth(300.4)
    expect(document.documentElement.style.getPropertyValue(SIDEBAR_WIDTH_VAR)).toBe(
      "300px",
    )
    expect(localStorage.getItem("zwai.sidebar.width")).toBeNull()
  })
})
