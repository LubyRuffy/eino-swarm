import { Input } from "@/components/ui/input"
import { SelectItem } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import type { MemoryNotify, Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import {
  Field,
  SettingsChoice,
  SettingsPage,
  SettingsSection,
} from "./settings-field"

/** The install-wide memory budget. Per-project memory is switched on in the
 *  project dialog; these numbers decide what it costs when it is. */
export function MemorySettings({
  settings,
  onChange,
  query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const update = (patch: Partial<Settings["memory"]>) =>
    onChange({ ...settings, memory: { ...settings.memory, ...patch } })

  return (
    <SettingsPage
      title={t("settings.memory.title")}
      description={t("settings.memory.desc")}
    >
      <SettingsSection title={t("settings.memory.when")}>
        <Field
          query={query}
          label={t("settings.memory.enabled")}
          hint={t("settings.memory.enabledHint")}
        >
          <Switch
            checked={settings.memory.enabled}
            onCheckedChange={(enabled) => update({ enabled })}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.memory.autoReview")}
          hint={t("settings.memory.autoReviewHint")}
        >
          <Switch
            checked={settings.memory.auto_review}
            disabled={!settings.memory.enabled}
            onCheckedChange={(auto_review) => update({ auto_review })}
          />
        </Field>
        <SettingsChoice
          query={query}
          search={[t("settings.memory.after"), "notifications", "transcript"]}
          label={t("settings.memory.after")}
          hint={t("settings.memory.afterHint")}
          value={settings.memory.notifications || "on"}
          onValueChange={(notifications) =>
            update({ notifications: notifications as MemoryNotify })
          }
        >
          <SelectItem value="on">{t("settings.memory.notifyOn")}</SelectItem>
          <SelectItem value="verbose">
            {t("settings.memory.notifyVerbose")}
          </SelectItem>
          <SelectItem value="off">{t("settings.memory.notifyOff")}</SelectItem>
        </SettingsChoice>
      </SettingsSection>

      <SettingsSection title={t("settings.memory.budget")}>
        <Field
          query={query}
          label={t("settings.memory.charLimit")}
          hint={t("settings.memory.charLimitHint")}
        >
          <Input
            type="number"
            min={200}
            value={settings.memory.char_limit}
            onChange={(e) => update({ char_limit: Number(e.target.value) })}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.memory.entryMax")}
          hint={t("settings.memory.entryMaxHint")}
        >
          <Input
            type="number"
            min={80}
            value={settings.memory.entry_max}
            onChange={(e) => update({ entry_max: Number(e.target.value) })}
          />
        </Field>
        <Field
          query={query}
          label={t("settings.memory.reviewRounds")}
          hint={t("settings.memory.reviewRoundsHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.memory.review_max_iterations}
            onChange={(e) =>
              update({ review_max_iterations: Number(e.target.value) })
            }
          />
        </Field>
        <Field
          query={query}
          label={t("settings.memory.skillsIndex")}
          hint={t("settings.memory.skillsIndexHint")}
        >
          <Input
            type="number"
            min={1}
            value={settings.memory.skills_index_max}
            onChange={(e) =>
              update({ skills_index_max: Number(e.target.value) })
            }
          />
        </Field>
      </SettingsSection>
    </SettingsPage>
  )
}
