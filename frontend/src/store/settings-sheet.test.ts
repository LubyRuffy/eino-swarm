import { describe, expect, it } from "vitest"

import { openSettings, useSettingsSheet } from "./settings-sheet"

describe("settings sheet", () => {
  it("opens General from the sidebar and Models from a provider shortcut", () => {
    useSettingsSheet.setState({ open: false, section: "general" })
    openSettings()
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "general",
    })
    openSettings("models")
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "models",
    })
  })
})
