import { describe, expect, it } from "vitest"

import { composerToolsMenuPlacement } from "./composer-menu"

describe("composer tools menu placement", () => {
  it("clears the composer box and keeps a short window scrollable", () => {
    expect(composerToolsMenuPlacement({ top: 700 }, { top: 640 })).toEqual({ sideOffset: 68, maxHeight: 320 })
    expect(composerToolsMenuPlacement({ top: 40 }, { top: 40 })).toEqual({ sideOffset: 8, maxHeight: 96 })
    expect(composerToolsMenuPlacement({ top: 100 }, { top: 40 }).maxHeight).toBe(96)
  })
})
