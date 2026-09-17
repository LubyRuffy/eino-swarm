import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import {
  SettingsPage,
  SettingsSection,
  settingsMatch,
} from "./settings-field"

/** Install-wide personal preferences. A project's instruction is the
 *  business context and wins when the two conflict. */
export function PersonalityTab({
  settings,
  onChange,
  query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const value = settings.personality?.instructions ?? ""
  const label = t("settings.personality.instructions")
  const hint = t("settings.personality.hint")

  return (
    <SettingsPage
      title={t("settings.personality.title")}
      description={t("settings.personality.desc")}
    >
      <SettingsSection>
        {settingsMatch(query, label, hint, t("settings.personality.title")) ? (
          <div className="grid gap-2 px-4 py-3.5" data-settings-row="">
            <Label htmlFor="personality-instructions">{label}</Label>
            <p className="text-xs leading-relaxed text-muted-foreground">
              {hint}
            </p>
            <Textarea
              id="personality-instructions"
              rows={10}
              className="min-h-40 resize-y border border-input bg-background shadow-sm focus-visible:ring-2 focus-visible:ring-ring"
              value={value}
              onChange={(e) =>
                onChange({
                  ...settings,
                  personality: { instructions: e.target.value },
                })
              }
            />
          </div>
        ) : null}
      </SettingsSection>
    </SettingsPage>
  )
}
