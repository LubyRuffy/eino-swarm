import { describe, expect, it } from "vitest"

import { composerToolsMenuPlacement, filterRemoteTools, toolHoverText, toolMenuMatches } from "./composer-menu"

describe("composer tools menu placement", () => {
  it("clears the composer box and keeps a short window scrollable", () => {
    expect(composerToolsMenuPlacement({ top: 700 }, { top: 640 })).toEqual({ sideOffset: 68, maxHeight: 320 })
    expect(composerToolsMenuPlacement({ top: 40 }, { top: 40 })).toEqual({ sideOffset: 8, maxHeight: 96 })
    expect(composerToolsMenuPlacement({ top: 100 }, { top: 40 }).maxHeight).toBe(96)
  })
})

describe("composer tools menu filter", () => {
  const tools = [{ name: "lookup" }, { name: "status" }]

  it("matches a tool or a connection and keeps a blank query open", () => {
    expect(toolMenuMatches("  ", ["Read file"])).toBe(true)
    expect(toolMenuMatches("FILE", ["Read file", "read"])).toBe(true)
    expect(toolMenuMatches("python", ["Read file", "read"])).toBe(false)
    expect(filterRemoteTools(tools, "", "Desk", "desk")).toEqual({ shown: true, tools })
    expect(filterRemoteTools(tools, "desk", "Desk", "desk").tools).toEqual(tools)
    expect(filterRemoteTools(tools, "LOOK", "Desk", "desk").tools).toEqual([{ name: "lookup" }])
    expect(filterRemoteTools(tools, "missing", "Desk", "desk").shown).toBe(false)
    expect(filterRemoteTools([], "desk", "Desk", "desk")).toEqual({ shown: true, tools: [] })
    expect(toolHoverText("read", "Read a file")).toBe("read\nRead a file")
    expect(toolHoverText("read", "  ")).toBe("read")
    expect(toolHoverText("read", "read")).toBe("read")
  })
})
