import { useRef } from "react"
import { create } from "zustand"

import type { SettingsSectionId } from "@/components/app/settings-dialog"
import type { TranscriptState } from "@/lib/transcript"
import { useApp } from "@/store/app"

/** Settings is a full-page sheet. The store lives outside App so opening
 *  from the sidebar, ⌘,, ⌘K, the model picker, or the unconfigured banner
 *  cannot re-render the transcript. */
export const useSettingsSheet = create<{
  open: boolean
  section: SettingsSectionId
  /** data-settings-key to scroll into view once the page is painted.
   *  Empty means land on the section and leave focus alone. */
  focus: string
}>(() => ({ open: false, section: "general", focus: "" }))

export function openSettings(section: SettingsSectionId = "general", focus = "") {
  useSettingsSheet.setState({ open: true, section, focus })
}

export function closeSettings() {
  useSettingsSheet.setState({
    open: false,
    section: "general",
    focus: "",
  })
}

/** While Settings covers the window, keep the snapshot the conversation
 *  already painted. The sheet hides that tree; reconciling every streamed
 *  token still ran on the main thread and delayed the next settings click. */
export function transcriptWhileCovered(
  open: boolean,
  live: TranscriptState,
  held: { current: TranscriptState | undefined },
): TranscriptState {
  if (!open) {
    held.current = undefined
    return live
  }
  if (held.current === undefined) held.current = live
  return held.current
}

export function useCoveredTranscript(): TranscriptState {
  const open = useSettingsSheet((s) => s.open)
  const held = useRef<TranscriptState | undefined>(undefined)
  return useApp((s) => transcriptWhileCovered(open, s.transcript, held))
}
