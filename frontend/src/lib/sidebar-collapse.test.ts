import { afterEach, describe, expect, it } from "vitest"

import {
  isProjectExpanded,
  readProjectExpanded,
  readSectionExpanded,
  runningProjectIds,
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

  it("opens a folder that has a running conversation", () => {
    expect(
      isProjectExpanded("pj_busy", {
        activeProjectId: "pj_open",
        selectedId: undefined,
        runningProjectIds: ["pj_busy"],
        overrides: {},
      }),
    ).toBe(true)
    expect(
      isProjectExpanded("pj_busy", {
        activeProjectId: "pj_open",
        selectedId: undefined,
        runningProjectIds: ["pj_busy"],
        overrides: { pj_busy: false },
      }),
    ).toBe(false)
  })
})

describe("runningProjectIds", () => {
  it("collects folders with a live conversation, including the overlay id", () => {
    expect(
      runningProjectIds(
        [
          { id: "th_1", project_id: "pj_a", running: true },
          { id: "th_2", project_id: "pj_b", running: false },
          { id: "th_3", project_id: "", running: true },
        ],
        "th_2",
      ),
    ).toEqual(new Set(["pj_a", "pj_b"]))
  })

  it("opens a folder that only has a parked wait", () => {
    expect(
      runningProjectIds(
        [
          { id: "th_wait", project_id: "pj_wait", running: false },
          { id: "th_idle", project_id: "pj_idle", running: false },
        ],
        undefined,
        ["th_wait"],
      ),
    ).toEqual(new Set(["pj_wait"]))
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
