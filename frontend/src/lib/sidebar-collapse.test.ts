import { afterEach, describe, expect, it } from "vitest"

import {
  isProjectExpanded,
  readProjectExpanded,
  readSectionExpanded,
  writeProjectExpanded,
  writeSectionExpanded,
} from "./sidebar-collapse"

afterEach(() => {
  localStorage.clear()
})

describe("isProjectExpanded", () => {
  it("opens the active conversation's project and leaves the others closed", () => {
    expect(
      isProjectExpanded("pj_open", {
        activeProjectId: "pj_open",
        selectedId: undefined,
        overrides: {},
      }),
    ).toBe(true)
    expect(
      isProjectExpanded("pj_other", {
        activeProjectId: "pj_open",
        selectedId: undefined,
        overrides: {},
      }),
    ).toBe(false)
  })

  it("opens a selected project that has no conversation yet", () => {
    expect(
      isProjectExpanded("pj_new", {
        activeProjectId: undefined,
        selectedId: "pj_new",
        overrides: {},
      }),
    ).toBe(true)
  })

  it("lets an explicit collapse win even for the open conversation's project", () => {
    expect(
      isProjectExpanded("pj_open", {
        activeProjectId: "pj_open",
        selectedId: "pj_open",
        overrides: { pj_open: false },
      }),
    ).toBe(false)
    expect(
      isProjectExpanded("pj_other", {
        activeProjectId: "pj_open",
        selectedId: undefined,
        overrides: { pj_other: true },
      }),
    ).toBe(true)
  })
})

describe("project expand storage", () => {
  it("round-trips a map and treats garbage as nothing remembered", () => {
    expect(readProjectExpanded()).toEqual({})
    writeProjectExpanded({ pj_a: true, pj_b: false })
    expect(readProjectExpanded()).toEqual({ pj_a: true, pj_b: false })
    localStorage.setItem("zwai.sidebar.project-expanded", "nope")
    expect(readProjectExpanded()).toEqual({})
    localStorage.setItem("zwai.sidebar.project-expanded", "[]")
    expect(readProjectExpanded()).toEqual({})
  })
})

describe("section expand storage", () => {
  it("starts with Pinned, Projects and Recents open", () => {
    expect(readSectionExpanded()).toEqual({
      pinned: true,
      projects: true,
      recents: true,
    })
  })

  it("round-trips a fold and treats garbage as the open default", () => {
    writeSectionExpanded({ pinned: true, projects: false, recents: false })
    expect(readSectionExpanded()).toEqual({
      pinned: true,
      projects: false,
      recents: false,
    })
    localStorage.setItem("zwai.sidebar.section-expanded", "nope")
    expect(readSectionExpanded().recents).toBe(true)
    localStorage.setItem("zwai.sidebar.section-expanded", "[]")
    expect(readSectionExpanded().projects).toBe(true)
  })

  it("ignores unknown keys and non-booleans so a future field cannot collapse Recents", () => {
    localStorage.setItem(
      "zwai.sidebar.section-expanded",
      JSON.stringify({ recents: false, extra: true, pinned: "nope" }),
    )
    expect(readSectionExpanded()).toEqual({
      pinned: true,
      projects: true,
      recents: false,
    })
  })
})
