import { describe, expect, it } from "vitest"

import { closeSettings, openSettings, useSettingsSheet } from "./settings-sheet"

const closed = {
  open: false,
  section: "general" as const,
  focus: "",
  budgetReturn: null,
}

describe("settings sheet", () => {
  it("opens General from the sidebar and Models from a provider shortcut", () => {
    useSettingsSheet.setState({ ...closed, focus: "stale" })
    openSettings()
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "general",
      focus: "",
      budgetReturn: null,
    })
    openSettings("models")
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "models",
      focus: "",
      budgetReturn: null,
    })
    openSettings("swarm", "max_completion_tokens")
    expect(useSettingsSheet.getState()).toEqual({
      open: true,
      section: "swarm",
      focus: "max_completion_tokens",
      budgetReturn: null,
    })
  })

  it("offers a retry only after the output-budget link's sheet closes", () => {
    useSettingsSheet.setState(closed)
    openSettings("swarm", "max_completion_tokens", {
      threadId: "th",
      turnId: "turn-1",
    })
    expect(useSettingsSheet.getState().budgetReturn).toEqual({
      threadId: "th",
      turnId: "turn-1",
      phase: "open",
    })
    closeSettings()
    expect(useSettingsSheet.getState()).toEqual({
      open: false,
      section: "general",
      focus: "",
      budgetReturn: { threadId: "th", turnId: "turn-1", phase: "back" },
    })
    openSettings()
    expect(useSettingsSheet.getState().budgetReturn).toEqual({
      threadId: "th",
      turnId: "turn-1",
      phase: "back",
    })
    closeSettings()
    expect(useSettingsSheet.getState().budgetReturn?.phase).toBe("back")
  })
})
