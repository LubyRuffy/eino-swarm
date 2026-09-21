import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { chromeTypeClass } from "@/lib/chrome-type"
import {
  auxiliaryModelChoices,
  groupModels,
  modelChoiceId,
  parseModelChoiceId,
} from "@/lib/models"
import { defaultSearchSettings, type SearchSettings, type Settings } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"

import {
  Field,
  SettingsRow,
  SettingsSection,
  settingsSelectTriggerClass,
} from "./settings-field"

/** Keyword search is always on. This switch is the extra endpoint call. */
export function SearchSettingsPanel({
  settings,
  onChange,
  query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const search = defaultSearchSettings(settings.search)
  const update = (patch: Partial<SearchSettings>) =>
    onChange({
      ...settings,
      search: { ...search, ...patch },
    })
  const choices = extraChoice(
    auxiliaryModelChoices(settings.models.providers),
    search.embedding_provider,
    search.embedding_model,
    settings.models.default,
  )
  const groups = groupModels(
    choices.map((c) => ({
      provider_id: c.providerId,
      provider_label: c.providerLabel,
      model: c.model,
      label: c.label,
    })),
  )
  const pinned = choices.find(
    (c) =>
      c.model === search.embedding_model &&
      (!search.embedding_provider || c.providerId === search.embedding_provider),
  )

  return (
    <SettingsSection
      title={t("settings.searchEmbed.title")}
      description={t("settings.searchEmbed.desc")}
    >
      <Field
        query={query}
        label={t("settings.searchEmbed.embedding")}
        hint={t("settings.searchEmbed.embeddingHint")}
      >
        <Switch
          checked={search.embedding}
          onCheckedChange={(embedding) => update({ embedding })}
          aria-label={t("settings.searchEmbed.embedding")}
        />
      </Field>
      {search.embedding ? (
        <SettingsRow
          query={query}
          search={[
            t("settings.searchEmbed.model"),
            t("settings.searchEmbed.modelHint"),
            "embedding",
          ]}
          label={t("settings.searchEmbed.model")}
          hint={pinned ? pinned.label : t("settings.searchEmbed.modelHint")}
          badge={<Badge variant="default">{t("settings.searchEmbed.badge")}</Badge>}
        >
          {choices.length > 0 ? (
            <Select
              value={
                search.embedding_model
                  ? modelChoiceId(
                      search.embedding_provider || settings.models.default,
                      search.embedding_model,
                    )
                  : undefined
              }
              onValueChange={(id) => {
                const next = parseModelChoiceId(id)
                update({
                  embedding_provider: next.providerId,
                  embedding_model: next.model,
                })
              }}
            >
              <SelectTrigger
                aria-label={t("settings.searchEmbed.modelAria")}
                className={settingsSelectTriggerClass}
              >
                <SelectValue placeholder={t("settings.searchEmbed.pick")} />
              </SelectTrigger>
              <SelectContent>
                {groups.map((g) => (
                  <SelectGroup key={g.providerId}>
                    {groups.length > 1 ? <SelectLabel>{g.label}</SelectLabel> : null}
                    {g.models.map((m) => {
                      const id = modelChoiceId(m.provider_id, m.model)
                      return (
                        <SelectItem key={id} value={id}>
                          {m.label || m.model}
                        </SelectItem>
                      )
                    })}
                  </SelectGroup>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <Input
              value={search.embedding_model}
              onChange={(e) => update({ embedding_model: e.target.value })}
              aria-label={t("settings.searchEmbed.modelAria")}
              placeholder={t("settings.searchEmbed.pick")}
              className={cn("h-[32px] w-56", chromeTypeClass)}
            />
          )}
        </SettingsRow>
      ) : null}
    </SettingsSection>
  )
}

function extraChoice(
  choices: ReturnType<typeof auxiliaryModelChoices>,
  provider?: string,
  model?: string,
  fallbackProvider?: string,
) {
  const p = provider?.trim() ?? ""
  const m = model?.trim() ?? ""
  if (!m || choices.some((c) => (!p || c.providerId === p) && c.model === m)) {
    return choices
  }
  return [
    ...choices,
    {
      providerId: p || fallbackProvider || "",
      model: m,
      label: m,
      providerLabel: p || fallbackProvider || "",
    },
  ]
}
