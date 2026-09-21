import { Badge } from "@/components/ui/badge"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  TITLE_MODEL_AUTO,
  auxiliaryModelChoices,
  groupModels,
  isTitleModelAuto,
  modelChoiceId,
  parseModelChoiceId,
  titleModelChoiceId,
} from "@/lib/models"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import {
  SettingsRow,
  SettingsSection,
  settingsSelectTriggerClass,
} from "./settings-field"

/** Jobs that are not the conversation itself. They follow the conversation's
 *  model until someone pins a cheaper (or different) one. */
export function AuxiliaryModels({
  settings,
  onChange,
  query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const choices = auxiliaryModelChoices(settings.models.providers)
  const listed = extraChoice(
    extraChoice(choices, settings.swarm.title_provider, settings.swarm.title_model, settings.models.default),
    settings.swarm.compact_provider,
    settings.swarm.compact_model,
    settings.models.default,
  )

  return (
    <SettingsSection
      title={t("settings.aux.title")}
      description={t("settings.aux.desc")}
    >
      <AuxiliaryPin
        query={query}
        title={t("settings.aux.titleGen")}
        badge={t("settings.aux.badge")}
        ariaLabel={t("settings.aux.titleAria")}
        provider={settings.swarm.title_provider}
        model={settings.swarm.title_model}
        choices={listed}
        onPin={(provider, model) =>
          onChange({
            ...settings,
            swarm: { ...settings.swarm, title_provider: provider, title_model: model },
          })
        }
      />
      <AuxiliaryPin
        query={query}
        title={t("settings.aux.compact")}
        badge={t("settings.aux.badge")}
        ariaLabel={t("settings.aux.compactAria")}
        provider={settings.swarm.compact_provider}
        model={settings.swarm.compact_model}
        choices={listed}
        onPin={(provider, model) =>
          onChange({
            ...settings,
            swarm: { ...settings.swarm, compact_provider: provider, compact_model: model },
          })
        }
      />
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
  if (
    !m ||
    choices.some((c) => (!p || c.providerId === p) && c.model === m)
  ) {
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

function AuxiliaryPin({
  query,
  title,
  badge,
  ariaLabel,
  provider,
  model,
  choices,
  onPin,
}: {
  query: string
  title: string
  badge: string
  ariaLabel: string
  provider?: string
  model?: string
  choices: ReturnType<typeof auxiliaryModelChoices>
  onPin: (provider: string, model: string) => void
}) {
  const t = useT()
  const auto = isTitleModelAuto(provider, model)
  const pinned = choices.find(
    (c) => c.providerId === provider && c.model === model,
  )
  const subtitle = auto
    ? t("settings.aux.auto")
    : pinned
      ? pinned.label
      : model || t("settings.aux.auto")
  const groups = groupModels(
    choices.map((c) => ({
      provider_id: c.providerId,
      provider_label: c.providerLabel,
      model: c.model,
      label: c.label,
    })),
  )

  return (
    <SettingsRow
      query={query}
      search={[title, subtitle, "auxiliary"]}
      label={title}
      hint={subtitle}
      badge={<Badge variant="default">{badge}</Badge>}
    >
      {choices.length > 1 ? (
        <Select
          value={titleModelChoiceId(provider, model, choices)}
          onValueChange={(id) => {
            if (id === TITLE_MODEL_AUTO) {
              onPin("", "")
              return
            }
            const next = parseModelChoiceId(id)
            onPin(next.providerId, next.model)
          }}
        >
          <SelectTrigger aria-label={ariaLabel} className={settingsSelectTriggerClass}>
            <SelectValue placeholder={t("settings.aux.automatic")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={TITLE_MODEL_AUTO}>
              {t("settings.aux.auto")}
            </SelectItem>
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
      ) : null}
    </SettingsRow>
  )
}
