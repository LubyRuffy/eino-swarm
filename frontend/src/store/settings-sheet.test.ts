import { describe, expect, it } from "vitest"

import { openSettings, useSettingsSheet } from "./settings-sheet"

describe("settings sheet", () => {
  it("opens General from the sidebar and Models from a provider shortcut", () => {
    useSettingsSheet.setState({ open: false, section: "general", focus: "stale" })
    openSettings()
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "general",
      focus: "",
    })
    openSettings("models")
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "models",
      focus: "",
    })
    openSettings("swarm", "max_completion_tokens")
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "swarm",
      focus: "max_completion_tokens",
    })
  })
})
