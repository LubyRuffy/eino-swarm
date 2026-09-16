import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import type { MemoryNotify, Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import {
  Field,
  SettingsPage,
  SettingsSection,
  settingsMatch,
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
        {settingsMatch(
          query,
          t("settings.memory.after"),
          "notifications",
          "transcript",
        ) ? (
          <div
            className="flex items-start justify-between gap-6 px-4 py-3.5"
            data-settings-row=""
          >
            <div className="min-w-0 flex-1">
              <p className="text-sm font-medium">{t("settings.memory.after")}</p>
              <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                {t("settings.memory.afterHint")}
              </p>
            </div>
            <div className="w-64 shrink-0">
              <Select
                value={settings.memory.notifications || "on"}
                onValueChange={(notifications) =>
                  update({ notifications: notifications as MemoryNotify })
                }
              >
                <SelectTrigger
                  aria-label={t("settings.memory.after")}
                  className="h-9 text-sm"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="on">{t("settings.memory.notifyOn")}</SelectItem>
                  <SelectItem value="verbose">
                    {t("settings.memory.notifyVerbose")}
                  </SelectItem>
                  <SelectItem value="off">{t("settings.memory.notifyOff")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        ) : null}
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
