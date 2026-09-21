import { create } from "zustand"

import type { SettingsSectionId } from "@/components/app/settings-dialog"

/** Settings is a full-page sheet. The store lives outside App so opening
 *  from the sidebar, ⌘,, ⌘K, the model picker, or the unconfigured banner
 *  cannot re-render the transcript. */
export const useSettingsSheet = create<{
  open: boolean
  section: SettingsSectionId
}>(() => ({ open: false, section: "general" }))

export function openSettings(section: SettingsSectionId = "general") {
  useSettingsSheet.setState({ open: true, section })
}
