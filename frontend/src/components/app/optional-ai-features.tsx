import type { ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { policyAllows } from "@/lib/model-policy"
import { defaultSpeechSettings } from "@/lib/speech-types"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { FunctionalModelField } from "./functional-models"
import { Field, SettingsRow, SettingsSection } from "./settings-field"
import { SpeechSettings, speechFeatureEnabled, speechSettingsSearch } from "./settings-speech"

type Props = {
  settings: Settings
  onChange: (settings: Settings) => void
  onFlush: () => Promise<void>
  query: string
}

export function OptionalAIFeatures({ settings, onChange, onFlush, query }: Props) {
  const t = useT()
  const snapshot = useApp(state => state.meta?.speech)
  const mode = useApp(state => state.meta?.product_mode)
  const configured = useApp(state => state.meta?.configured)
  const { swarm, model_policy: policy } = settings
  const gateway = (policy?.mode ?? (mode === "fofa" ? "gateway" : "local")) === "gateway"
  const speech = defaultSpeechSettings(settings.speech)
  const update = (patch: Partial<Settings["swarm"]>) => onChange({ ...settings, swarm: { ...swarm, ...patch } })
  const title = swarm.auto_title !== false
  const prediction = swarm.composer_predictions !== false
  const graph = swarm.graph !== false && swarm.graph_auto_record !== false
  const translation = swarm.selection_translation !== false
  const switchFor = (label: string, checked: boolean, set: (checked: boolean) => void) =>
    <Field label={t("aiFeatures.enabled")}>
      <Switch aria-label={label} checked={checked} onCheckedChange={set} />
    </Field>
  return <SettingsSection title={t("aiFeatures.title")}>
    <FeatureDrawer query={query} label={t("settings.swarm.autoTitle")} description={t("aiFeatures.titleHint")}
      enabled={title} ready={configured !== false}>
      {switchFor(t("settings.swarm.autoTitle"), title, auto_title => update({ auto_title }))}
      <p className="px-4 py-3 text-sm text-muted-foreground">{t(gateway ? "aiFeatures.gatewayNamingModel" : "aiFeatures.namingModel")}</p>
    </FeatureDrawer>
    <FeatureDrawer query={query} label={t("settings.swarm.predictions")} description={t("aiFeatures.predictionHint")}
      enabled={prediction} ready={configured !== false}>
      {switchFor(t("settings.swarm.predictions"), prediction, composer_predictions => update({ composer_predictions }))}
      {prediction ? <FunctionalModelField role="conversation" prediction settings={settings} onChange={onChange} /> : null}
    </FeatureDrawer>
    <FeatureDrawer query={query} label={t("aiFeatures.graph")} description={t("aiFeatures.graphHint")} enabled={graph}>
      {switchFor(t("aiFeatures.graph"), graph, graph_auto_record =>
        update(graph_auto_record ? { graph: true, graph_auto_record } : { graph_auto_record }))}
    </FeatureDrawer>
    <FeatureDrawer query={query} label={t("modelPolicy.translation")} description={t("aiFeatures.translationHint")}
      enabled={translation} ready={configured !== false && policyAllows(policy, "translation")}>
      {switchFor(t("modelPolicy.translation"), translation, selection_translation => update({ selection_translation }))}
      {translation ? <FunctionalModelField role="translation" settings={settings} onChange={onChange} /> : null}
    </FeatureDrawer>
    {(["input", "output"] as const).map(kind => {
      const role = kind === "input" ? "transcription" : "speech"
      const enabled = speechFeatureEnabled(speech[kind], gateway, snapshot?.[kind])
      return <FeatureDrawer key={kind} query={query} label={t(kind === "input" ? "speech.input" : "speech.output")}
        description={t(kind === "input" ? "aiFeatures.inputHint" : "aiFeatures.outputHint")}
        enabled={enabled} ready={snapshot?.[kind].ready === true} search={speechSettingsSearch}>
        <SpeechSettings settings={settings} onChange={onChange} onFlush={onFlush} kind={kind}
          modelPicker={<FunctionalModelField role={role} settings={settings} onChange={onChange} />} />
      </FeatureDrawer>
    })}
  </SettingsSection>
}

function FeatureDrawer({ label, description, enabled, ready = true, query, search, children }: {
  label: string
  description: string
  enabled: boolean
  ready?: boolean
  query: string
  search?: string
  children: ReactNode
}) {
  const t = useT()
  const status = t(!enabled ? "aiFeatures.off" : ready ? "aiFeatures.on" : "aiFeatures.pending")
  return <Dialog>
    <SettingsRow label={label} hint={status} search={[description, search]} query={query}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm" type="button" aria-label={t("aiFeatures.configureFeature", { feature: label })}>
          {t("aiFeatures.configure")}
        </Button>
      </DialogTrigger>
    </SettingsRow>
    <DialogContent side="right">
      <header className="shrink-0 border-b border-border px-5 pb-4 pt-6 pe-12">
        <DialogTitle>{label}</DialogTitle>
        <DialogDescription className="mt-2">{description}</DialogDescription>
      </header>
      <div className="thin-scrollbar min-h-0 flex-1 overflow-y-auto px-4 py-3">
        {enabled && !ready ? <p role="status" className="px-4 pb-2 text-sm text-muted-foreground">{status}</p> : null}
        {children}
      </div>
    </DialogContent>
  </Dialog>
}
