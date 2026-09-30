import { create } from "zustand"

import type { SettingsSectionId } from "@/components/app/settings-dialog"
import { MAX_COMPLETION_SETTINGS_KEY } from "@/lib/output-budget"

/** The output-budget link opened Settings for this turn. `open` means the
 *  sheet is still up. `back` means the user closed it, so that error can
 *  offer a retry. Any other Settings entry leaves this alone. */
export type BudgetReturn = {
  threadId: string
  turnId: string
  phase: "open" | "back"
}

/** Settings is a full-page sheet. The store lives outside App so opening
 *  from the sidebar, ⌘,, ⌘K, the model picker, or the unconfigured banner
 *  cannot re-render the transcript. */
export const useSettingsSheet = create<{
  open: boolean
  section: SettingsSectionId
  /** data-settings-key to scroll into view once the page is painted.
   *  Empty means land on the section and leave focus alone. */
  focus: string
  budgetReturn: BudgetReturn | null
}>(() => ({ open: false, section: "general", focus: "", budgetReturn: null }))

export function openSettings(
  section: SettingsSectionId = "general",
  focus = "",
  origin?: { threadId: string; turnId: string },
) {
  const prev = useSettingsSheet.getState()
  const arm: BudgetReturn | null =
    focus === MAX_COMPLETION_SETTINGS_KEY && origin?.threadId && origin.turnId
      ? { threadId: origin.threadId, turnId: origin.turnId, phase: "open" }
      : prev.budgetReturn
  useSettingsSheet.setState({ open: true, section, focus, budgetReturn: arm })
}

export function closeSettings() {
  const prev = useSettingsSheet.getState()
  useSettingsSheet.setState({
    open: false,
    section: "general",
    focus: "",
    budgetReturn:
      prev.budgetReturn?.phase === "open"
        ? { ...prev.budgetReturn, phase: "back" }
        : prev.budgetReturn,
  })
}
