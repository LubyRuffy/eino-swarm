import { create } from "zustand"

import type { SettingsSectionId } from "@/components/app/settings-dialog"

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

export function openSettings(
  section: SettingsSectionId = "general",
  focus = "",
) {
  useSettingsSheet.setState({ open: true, section, focus })
}
