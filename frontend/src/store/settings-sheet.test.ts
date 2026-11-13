import { describe, expect, it } from "vitest"

import { closeSettings, openSettings, transcriptWhileCovered, useSettingsSheet } from "./settings-sheet"
import type { TranscriptState } from "@/lib/transcript"

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

  it("holds the painted conversation while Settings covers it", () => {
    const held: { current: TranscriptState | undefined } = { current: undefined }
    const first = { lastSeq: 1 } as TranscriptState
    const second = { lastSeq: 2 } as TranscriptState
    expect(transcriptWhileCovered(false, first, held)).toBe(first)
    expect(held.current).toBeUndefined()
    expect(transcriptWhileCovered(true, first, held)).toBe(first)
    expect(transcriptWhileCovered(true, second, held)).toBe(first)
    expect(transcriptWhileCovered(false, second, held)).toBe(second)
    expect(held.current).toBeUndefined()
  })
})
