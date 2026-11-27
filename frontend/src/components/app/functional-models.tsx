import { useId } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { SelectGroup, SelectItem, SelectLabel } from "@/components/ui/select"
import { functionalChoiceLabel, functionalGroupHeadings, functionalModelChoices, groupFunctionalChoices, type FunctionalSourceGroup } from "@/lib/functional-model-choices"
import type { MessageKey } from "@/lib/i18n"
import { configurableModelRoles, policyAllows, policyConfigurable, type ModelRef, type ModelRole } from "@/lib/model-policy"
import { modelChoiceId, parseModelChoiceId } from "@/lib/models"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { SettingsChoice, SettingsRow, SettingsSection } from "./settings-field"

const functionalModelLabels: Record<Exclude<ModelRole, "decisions">, MessageKey> = {
  conversation: "modelPolicy.conversation",
  title: "modelPolicy.titleRole",
  compact: "modelPolicy.compact",
  embedding: "modelPolicy.embedding",
  transcription: "modelPolicy.transcription",
  speech: "modelPolicy.speech",
  ocr: "modelPolicy.ocr",
  translation: "modelPolicy.translation",
  image: "modelPolicy.image",
}

const groupLabels: Record<FunctionalSourceGroup, MessageKey> = {
  gateway: "modelPolicy.group.gateway",
  third_party: "modelPolicy.group.thirdParty",
  local: "modelPolicy.group.local",
}

const optionalRoles: ModelRole[] = ["title", "translation", "transcription", "speech"]

type SettingsProps = {
  settings: Settings
  onChange: (value: Settings) => void
  query?: string
}

export function FunctionalModels({ settings, onChange, query }: SettingsProps) {
  const t = useT()
  if (!settings.model_policy?.configurable) return null
  return <SettingsSection title={t("modelPolicy.title")} description={t("modelPolicy.description")}>
    {configurableModelRoles.filter(role => !optionalRoles.includes(role) &&
      (role === "image" || role === "embedding" || policyConfigurable(settings.model_policy, role))).map(role =>
      <FunctionalModelField key={role} role={role} settings={settings} onChange={onChange} query={query} />
    )}
  </SettingsSection>
}

export function FunctionalModelField({ role, prediction, settings, onChange, query = "" }: SettingsProps & {
  role: Exclude<ModelRole, "decisions">
  prediction?: boolean
}) {
  const t = useT(), id = useId()
  const account = useApp(s => s.meta?.account_models)
  const models = useApp(s => s.models)
  const binding = prediction ? "composer_prediction" : role
  const label = prediction ? t("modelPolicy.model") : t(functionalModelLabels[role])
  const update = (ref?: ModelRef) => {
    const bindings = { ...(settings.models.bindings ?? settings.model_policy?.bindings) }
    if (ref) bindings[binding] = ref
    else delete bindings[binding]
    onChange({ ...settings, models: { ...settings.models, bindings },
      ...(role === "embedding" ? { search: { embedding: false, embedding_provider: "", embedding_model: "" } } : {}),
    })
  }
  if (!policyConfigurable(settings.model_policy, role)) {
    const managed = policyAllows(settings.model_policy, role)
    return <SettingsRow key={role} query={query} label={label}
      hint={t(managed ? "modelPolicy.roleManagedHint" : "modelPolicy.roleUnavailable")}>
      <span className="text-muted-foreground">{t(managed ? "modelPolicy.roleManaged" : "modelPolicy.unavailable")}</span>
      {role === "image" && (settings.models.bindings?.image ?? settings.model_policy?.bindings?.image)?.model
        ? <Button type="button" variant="ghost" size="sm" onClick={() => update()}>{t("modelPolicy.default")}</Button> : null}
    </SettingsRow>
  }
  const fallback = settings.model_policy?.defaults?.[role]
  const legacy = role === "embedding" && !fallback && settings.search?.embedding_model?.trim()
    ? { provider_id: settings.search.embedding_provider || settings.models.default, model: settings.search.embedding_model } : undefined
  const ref = (settings.models.bindings ?? settings.model_policy?.bindings)?.[binding] ?? legacy
  const off = role === "embedding" && ref?.provider_id === "" && ref.model === ""
  const choices = functionalModelChoices({
    role, policy: settings.model_policy, models, providers: settings.models.providers, account,
    selected: ref?.provider_id && ref.model ? ref : undefined,
  })
  const groups = groupFunctionalChoices(choices)
  const sections = groups.flatMap(group => functionalGroupHeadings(group.group, group.choices, t(groupLabels[group.group])))
  const names = choices.filter(choice => choice.providerId === ref?.provider_id).map(choice => choice.model)
  const value = off ? "__disabled" : ref?.provider_id && ref.model ? modelChoiceId(ref.provider_id, ref.model) : "__default"
  return <div key={role} data-settings-key={`model-${binding}`}>
    <SettingsChoice label={label}
      hint={role === "image" ? t("modelPolicy.imageHint") : role === "embedding" ? t("modelPolicy.embeddingHint") : undefined}
      query={query} value={value}
      ariaLabel={prediction || role === "image" ? label : `${label} ${t("modelPolicy.provider")}`}
      onValueChange={next => {
        // Radix reports an empty value when the current id is not mounted yet.
        // That is not a request to clear the saved model.
        if (!next) return
        if (next === "__disabled") update({ provider_id: "", model: "" })
        else if (next === "__default") update()
        else {
          const choice = parseModelChoiceId(next)
          if (!choice.providerId || !choice.model) return
          update({ provider_id: choice.providerId, model: choice.model })
        }
      }}>
      {role === "embedding" ? <SelectItem value="__disabled">{t("modelPolicy.off")}</SelectItem> : null}
      <SelectItem value="__default">{t(prediction ? settings.model_policy?.mode === "gateway" ? "aiFeatures.gatewayModel" : "aiFeatures.conversationModel" : role === "embedding" && !fallback ? "modelPolicy.defaultOff" : "modelPolicy.default")}{fallback ? ` · ${fallback.model}` : ""}</SelectItem>
      {sections.map(section => <SelectGroup key={section.key}>
        {sections.length > 1 ? <SelectLabel>{section.title}</SelectLabel> : null}
        {section.choices.map(choice => {
          const choiceId = modelChoiceId(choice.providerId, choice.model)
          return <SelectItem key={choiceId} value={choiceId}>{functionalChoiceLabel(choice, section.choices)}</SelectItem>
        })}
      </SelectGroup>)}
    </SettingsChoice>
    {ref?.provider_id && ref.model && role !== "image" ? <SettingsRow label={t("modelPolicy.model")} query={query} search={[label]}>
      <Input aria-label={`${label} ${t("modelPolicy.model")}`} key={`${ref.provider_id}:${ref.model}`}
        defaultValue={ref.model} list={`${id}-${role}`}
        onKeyDown={event => { if (event.key === "Enter") { event.preventDefault(); event.currentTarget.blur() } }}
        onBlur={event => {
          const model = event.target.value.trim()
          if (!model && role === "embedding") update({ provider_id: "", model: "" })
          else if (model && model !== ref.model) update({ ...ref, model })
          else event.target.value = ref.model
        }} />
      <datalist id={`${id}-${role}`}>{names.map(name => <option key={name} value={name} />)}</datalist>
      <Button type="button" variant="ghost" size="sm" onClick={() => update()}>{t("modelPolicy.default")}</Button>
    </SettingsRow> : null}
  </div>
}
