import { describe, expect, it } from "vitest"

import { closeSettings, openSettings, useSettingsSheet } from "./settings-sheet"

const closed = {
  open: false,
  section: "general" as const,
  focus: "",
}

describe("settings sheet", () => {
  it("opens General from the sidebar and Models from a provider shortcut", () => {
    useSettingsSheet.setState({ ...closed, focus: "stale" })
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

  it("clears the previous section and focus when the sheet closes", () => {
    useSettingsSheet.setState(closed)
    openSettings("swarm", "max_completion_tokens")
    closeSettings()
    expect(useSettingsSheet.getState()).toEqual(closed)
    openSettings()
    expect(useSettingsSheet.getState()).toEqual({ ...closed, open: true })
    closeSettings()
    expect(useSettingsSheet.getState()).toEqual(closed)
  })
})
