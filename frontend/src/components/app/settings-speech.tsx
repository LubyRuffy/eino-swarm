import { GatewayAudioSettings } from "./settings-audio-parameters"
import { refreshCapabilities } from "@/lib/refresh-capabilities"
import { policyAllows, policyConfigurable } from "@/lib/model-policy"
import { useCallback, useEffect, useId, useRef, useState, useSyncExternalStore, type ReactNode } from "react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { SelectItem } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { useApp } from "@/store/app"
import { toastError } from "@/store/toasts"
import { defaultSpeechSettings, type SpeechInputSettings, type SpeechOutputSettings } from "@/lib/speech-types"
import { listAudioInputDevices, speechPlayback, speechRecordingSupported, startSpeechRecording, type AudioInputDevice, type SpeechRecording } from "@/lib/speech-media"
import { synthesizeSpeech, transcribeSpeech } from "@/lib/speech-api"
import { speechFailureMessage } from "@/lib/speech-error"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { Field, SettingsChoice, SettingsRow, SettingsSection, settingsLabelClass, settingsMatch } from "./settings-field"

type Kind = "input" | "output"
type Connection = SpeechInputSettings | SpeechOutputSettings

export function speechFeatureEnabled(value: Connection, gatewayMode: boolean, snapshot?: { execution?: string; model?: string }) {
  return gatewayMode && (snapshot?.execution === "gateway" || !snapshot?.model) ? !value.gateway_disabled : value.enabled
}

export const speechSettingsSearch = "speech voice microphone audio asr tts transcription read aloud 语音 麦克风 录音 识别 朗读 音色"

export function SpeechSettings({ settings, onChange, onFlush, query: searchQuery = "", kind, modelPicker }: {
  settings: Settings; onChange: (settings: Settings) => void; onFlush: () => Promise<void>; query?: string; kind?: Kind; modelPicker?: ReactNode
}) {
  const t = useT()
  const query = settingsMatch(searchQuery, t("speech.title"), speechSettingsSearch) ? "" : searchQuery
  const local = useApp((store) => store.meta?.model_policy ? store.meta.model_policy.configurable : store.meta?.product_mode === "local")
  const mode = useApp((store) => store.meta?.product_mode)
  // Legacy LOCAL settings have no policy, but do have an explicit product
  // mode. An absent Gateway manifest still grants no voice capability.
  const legacyLocal = !settings.model_policy && mode === "local"
  const inputAllowed = kind ? kind === "input" : legacyLocal || policyAllows(settings.model_policy, "transcription")
  const outputAllowed = kind ? kind === "output" : legacyLocal || policyAllows(settings.model_policy, "speech")
  const snapshot = useApp((store) => store.meta?.speech)
  const raw = defaultSpeechSettings(settings.speech)
  // The provider/custom form has no account choice. A model-policy page keeps
  // source "account", which is the signed-in recognition or speech role.
  const legacyEndpoint = local && !settings.model_policy
  const speech = legacyEndpoint ? { ...raw,
    input: raw.input.source === "account" ? { ...raw.input, source: "custom" as const } : raw.input,
    output: raw.output.source === "account" ? { ...raw.output, source: "custom" as const } : raw.output,
  } : raw
  const [models, setModels] = useState<Partial<Record<Kind, string[]>>>({})
  const [discovering, setDiscovering] = useState<Kind>()
  const discovery = useRef<AbortController | undefined>(undefined)
  const id = useId()
  const connectionIdentity = JSON.stringify([speech.input.source, speech.input.provider_id, speech.input.base_url,
    speech.output.source, speech.output.provider_id, speech.output.base_url])
  useEffect(() => {
    discovery.current?.abort(); discovery.current = undefined; setDiscovering(undefined); setModels({})
  }, [connectionIdentity])
  useEffect(() => () => discovery.current?.abort(), [])
  const audioIdentity = JSON.stringify([snapshot?.input.source_id, snapshot?.input.model, snapshot?.output.source_id, snapshot?.output.model])
  useEffect(() => { if (snapshot?.input.execution === "gateway" || snapshot?.output.execution === "gateway") void refreshCapabilities().catch(() => undefined) }, [audioIdentity])
  if (!inputAllowed && !outputAllowed) return null
  const update = <K extends Kind>(kind: K, patch: Partial<(typeof speech)[K]>) => onChange({
    ...settings, speech: { ...speech, [kind]: { ...speech[kind], ...patch } },
  })
  const discover = async (kind: Kind) => {
    discovery.current?.abort()
    const owner = new AbortController()
    discovery.current = owner
    setDiscovering(kind)
    try {
      await onFlush()
      const response = await fetch("/api/audio/models", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ kind }), signal: owner.signal, redirect: "error" })
      const body = await response.json()
      if (!response.ok) throw new Error(typeof body.error === "string" ? body.error : t("speech.failed"))
      const names = Array.isArray(body.models) ? body.models.filter((name: unknown): name is string => typeof name === "string") : []
      if (!owner.signal.aborted) setModels((current) => ({ ...current, [kind]: names }))
    } catch (error) { if (!owner.signal.aborted) toastError(error instanceof Error ? error.message : t("speech.failed")) }
    finally { if (discovery.current === owner) { discovery.current = undefined; setDiscovering(undefined) } }
  }
  const connection = (kind: Kind, value: Connection) => settings.model_policy ? (
    policyConfigurable(settings.model_policy, kind === "input" ? "transcription" : "speech") ? <SettingsRow label={t("speech.model")} hint={snapshot?.[kind].ready ? (snapshot[kind].label || t("modelPolicy.default")) : t("speech.notReady")} query={query} /> : null
  ) : <>
    {local ? <>
      <SettingsChoice label={t("speech.source")} query={query} value={value.source} onValueChange={(source) => update(kind, { source: source as Connection["source"] })}>
        <SelectItem value="provider">{t("speech.provider")}</SelectItem><SelectItem value="custom">{t("speech.custom")}</SelectItem>
      </SettingsChoice>
      {value.source === "provider" ? <SettingsChoice label={t("speech.providerName")} query={query} value={value.provider_id || undefined} onValueChange={(provider_id) => update(kind, { provider_id })}>
        {settings.models.providers.map((provider) => <SelectItem value={provider.id} key={provider.id}>{provider.label || provider.id}</SelectItem>)}
      </SettingsChoice> : <>
        <Field label={t("speech.endpoint")} query={query} wide><Input value={value.base_url} onChange={(event) => update(kind, { base_url: event.target.value })} /></Field>
        <Field label={t("speech.key")} hint={value.has_api_key && value.api_key === undefined ? t("speech.keySaved") : undefined} query={query} wide>
          <Input type="password" autoComplete="new-password" value={value.api_key ?? ""} onChange={(event) => update(kind, { api_key: event.target.value })} />
        </Field>
        {value.has_api_key ? <SettingsRow label={t("speech.clearKey")} query={query}><Button type="button" variant="outline" size="sm" onClick={() => update(kind, { api_key: "", has_api_key: false })}>{t("speech.clearKey")}</Button></SettingsRow> : null}
      </>}
      <Field label={t("speech.model")} query={query} wide>
        <Input list={`${id}-${kind}-models`} value={value.model} onChange={(event) => update(kind, { model: event.target.value })} />
      </Field>
      <datalist id={`${id}-${kind}-models`}>{(models[kind] ?? []).map((model) => <option key={model} value={model} />)}</datalist>
      <SettingsRow label={t("speech.discover")} query={query}><Button type="button" variant="outline" size="sm" disabled={Boolean(discovering)} onClick={() => void discover(kind)}>{discovering === kind ? "…" : t("speech.discover")}</Button></SettingsRow>
    </> : <SettingsRow label={t("speech.model")} hint={t("speech.gatewayModel")} query={query}>
      <span className="text-sm text-muted-foreground">{snapshot?.[kind].label || t("speech.gateway")}</span>
    </SettingsRow>}
  </>
  const gatewayMode = (settings.model_policy?.mode ?? (mode === "fofa" ? "gateway" : "local")) === "gateway"
  const gateway = (kind: Kind) => snapshot?.[kind].execution === "gateway" || !local || (gatewayMode && !snapshot?.[kind].model)
  const gatewayConfigured = (kind: Kind) => snapshot?.[kind].execution === "gateway" && Boolean(snapshot[kind].model)
  const enabled = (target: Kind) => kind
    ? speechFeatureEnabled(speech[target], gatewayMode, snapshot?.[target])
    : gatewayMode && gateway(target) ? gatewayConfigured(target) && !speech[target].gateway_disabled : speech[target].enabled
  const enableSwitch = (kind: Kind) => {
    const followsGateway = gatewayMode && gateway(kind)
    const unavailable = !modelPicker && followsGateway && !gatewayConfigured(kind)
    return <Field label={t("speech.enabled")} hint={unavailable ? t("speech.notReady") : undefined} query={query}>
      <Switch checked={enabled(kind)} disabled={unavailable} aria-label={t(kind === "input" ? "speech.input" : "speech.output")} onCheckedChange={checked => {
        update(kind, modelPicker ? { enabled: checked, gateway_disabled: !checked }
          : followsGateway ? { gateway_disabled: !checked } : { enabled: checked })
      }} />
    </Field>
  }
  const advancedQuery = settingsMatch(query, t("speech.advanced")) ? "" : query
  const parameters = (kind: Kind) => {
    const view = snapshot?.[kind].audio
    return <GatewayAudioSettings query={query} view={view} role={kind === "input" ? "transcription" : "speech"} value={view ? speech.gateway_parameters?.[view.scope] ?? {} : {}} onChange={value => {
      if (view) onChange({ ...settings, speech: { ...speech, gateway_parameters: { ...speech.gateway_parameters, [view.scope]: value } } })
    }} labels={{ voice: t("speech.voice"), response_format: t("speech.outputFormat"), stream_format: t("speech.streamFormat"), language: t("speech.language"), speed: t("speech.speed"), instructions: t("speech.instructions"), prompt: t("speech.prompt"), follow: t("speech.audioFollow"), invalid: t("speech.audioInvalid"), unavailable: t("speech.audioUnavailable") }} />
  }
  return <>
    {inputAllowed ? <SettingsSection title={kind ? undefined : t("speech.input")} description={kind ? undefined : t("speech.description")}>
      {enableSwitch("input")}
      {!kind || enabled("input") ? <>
        {settings.model_policy && modelPicker ? modelPicker : connection("input", speech.input)}
        {gateway("input") ? parameters("input") : <>
          <Field label={t("speech.language")} query={query}><Input value={speech.input.language} placeholder={t("speech.autoLanguage")} onChange={(event) => update("input", { language: event.target.value })} /></Field>
          <Field label={t("speech.prompt")} query={query} wide><Input value={speech.input.prompt} onChange={(event) => update("input", { prompt: event.target.value })} /></Field>
        </>}
        <MicrophoneChoice query={query} value={speech.input.device_id} onChange={(device_id) => update("input", { device_id })} />
      </> : null}
    </SettingsSection> : null}
    {outputAllowed ? <SettingsSection title={kind ? undefined : t("speech.output")}>
      {enableSwitch("output")}
      {!kind || enabled("output") ? <>
        {settings.model_policy && modelPicker ? modelPicker : connection("output", speech.output)}
        {gateway("output") ? parameters("output") : <>
          <Field label={t("speech.voice")} query={query}><Input value={speech.output.voice} placeholder={t("speech.modelDefault")} onChange={(event) => update("output", { voice: event.target.value })} /></Field>
          <Field label={t("speech.speed")} query={query}><Input type="number" min={0.25} max={4} value={speech.output.speed} onChange={(event) => update("output", { speed: Number(event.target.value) })} /></Field>
          <SettingsChoice label={t("speech.outputFormat")} query={query} value={speech.output.format} onValueChange={(format) => update("output", { format: format as SpeechOutputSettings["format"] })}>
            <SelectItem value="mp3">MP3</SelectItem><SelectItem value="wav">WAV</SelectItem><SelectItem value="pcm">PCM</SelectItem>
          </SettingsChoice>
          <Field label={t("speech.instructions")} query={query} wide><Input value={speech.output.instructions} onChange={(event) => update("output", { instructions: event.target.value })} /></Field>
        </>}
      </> : null}
    </SettingsSection> : null}
    {(!kind || enabled(kind)) ? <>
    <details open={query ? true : undefined} className="rounded-xl border border-border bg-card [&:not(:has([data-settings-row]))]:hidden">
      <summary className={`${settingsLabelClass} cursor-pointer px-4 py-3`}>{t("speech.advanced")}</summary>
      <div className="px-4 pb-4">
        <SettingsSection>
          {inputAllowed ? <>
            <SettingsChoice label={t("speech.recordingFormat")} query={advancedQuery} value={speech.input.recording_format} onValueChange={(recording_format) => update("input", { recording_format: recording_format as "auto" | "wav" })}>
              <SelectItem value="auto">{t("speech.autoFormat")}</SelectItem><SelectItem value="wav">WAV</SelectItem>
            </SettingsChoice>
            <Field label={t("speech.duration")} query={advancedQuery}><Input type="number" min={1} value={speech.input.max_duration_seconds} onChange={(event) => update("input", { max_duration_seconds: Number(event.target.value) })} /></Field>
            <Field label={t("speech.inputTimeout")} query={advancedQuery}><Input type="number" min={1} value={speech.input.timeout_seconds} onChange={(event) => update("input", { timeout_seconds: Number(event.target.value) })} /></Field>
          </> : null}
          {outputAllowed ? <Field label={t("speech.outputTimeout")} query={advancedQuery}><Input type="number" min={1} value={speech.output.timeout_seconds} onChange={(event) => update("output", { timeout_seconds: Number(event.target.value) })} /></Field> : null}
          {([
            ["input_bytes", "speech.inputLimit", 1 << 20], ["output_bytes", "speech.outputLimit", 1 << 20],
            ["chunk_chars", "speech.chunkLimit", 1], ["staging_ttl_seconds", "speech.ttl", 1],
          ] as const).map(([key, label, unit]) => <Field key={key} label={t(label)} query={advancedQuery}><Input type="number" min={1} value={speech.limits[key] / unit} onChange={(event) => onChange({ ...settings, speech: { ...speech, limits: { ...speech.limits, [key]: Number(event.target.value) * unit } } })} /></Field>)}
        </SettingsSection>
      </div>
    </details>
    {!query ? <SpeechSettingsTest kind={kind} speech={speech} inputEnabled={inputAllowed && enabled("input")} outputEnabled={outputAllowed && enabled("output")} onFlush={onFlush} /> : null}
    </> : null}
  </>
}

function MicrophoneChoice({ value, onChange, query }: { value: string; onChange: (id: string) => void; query: string }) {
  const t = useT()
  const [devices, setDevices] = useState<AudioInputDevice[]>([])
  const live = useRef(true)
  const pending = useRef<{ requestingAccess: boolean } | undefined>(undefined)
  const refresh = useCallback((requestingAccess = false) => {
    if (pending.current && (!requestingAccess || pending.current.requestingAccess)) return
    const owner = { requestingAccess }
    pending.current = owner
    const permission = requestingAccess ? navigator.mediaDevices?.getUserMedia?.({ audio: true, video: false }) : undefined
    void listAudioInputDevices(permission).then((next) => {
      if (!live.current || pending.current !== owner) return
      setDevices((current) => current.length === next.length && current.every((device, index) => device.id === next[index]?.id && device.label === next[index]?.label) ? current : next)
    }).catch(() => undefined).finally(() => {
      if (pending.current === owner) pending.current = undefined
    })
  }, [])
  useEffect(() => {
    live.current = true
    refresh()
    const media = navigator.mediaDevices
    const onDeviceChange = () => refresh()
    media?.addEventListener?.("devicechange", onDeviceChange)
    return () => { live.current = false; media?.removeEventListener?.("devicechange", onDeviceChange) }
  }, [refresh])
  const missing = Boolean(value) && !devices.some((device) => device.id === value)
  const hasDeviceLabels = devices.some((device) => device.label)
  // An empty select value keeps the default option actionable while the saved
  // preference is temporarily unavailable in this browser.
  return <SettingsChoice label={t("speech.microphone")} hint={t(missing && hasDeviceLabels ? "speech.microphoneFallback" : "speech.microphoneHint")} query={query} value={missing ? "" : value || "system"} placeholder={t("speech.systemDefault")} onValueChange={(id) => onChange(id === "system" ? "" : id)} onOpenChange={(open) => {
    if (open) refresh(!hasDeviceLabels)
  }}>
    <SelectItem value="system">{t("speech.systemDefault")}</SelectItem>
    {devices.map((device) => <SelectItem key={device.id} value={device.id}>{device.label || t("speech.microphone")}</SelectItem>)}
  </SettingsChoice>
}

function SpeechSettingsTest({ kind, speech, inputEnabled, outputEnabled, onFlush }: { kind?: Kind; speech: ReturnType<typeof defaultSpeechSettings>; inputEnabled: boolean; outputEnabled: boolean; onFlush: () => Promise<void> }) {
  const t = useT()
  const [recording, setRecording] = useState(false)
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState("")
  const [text, setText] = useState("")
  const capture = useRef<SpeechRecording | undefined>(undefined)
  const controller = useRef<AbortController | undefined>(undefined)
  const snapshot = useApp((store) => store.meta?.speech)
  const playback = useSyncExternalStore(speechPlayback.subscribe, speechPlayback.getSnapshot)
  const playing = playback.key === "settings:speech"
  const inputHint = recording ? undefined : !inputEnabled ? t("speech.testInputDisabled")
    : !speechRecordingSupported() ? t("speech.testRecordingUnsupported")
    : snapshot?.input.ready === false ? t("speech.testNotReady") : undefined
  const outputHint = playing ? undefined : !outputEnabled ? t("speech.testOutputDisabled")
    : snapshot?.output.ready === false ? t("speech.testNotReady")
    : !text.trim() ? t("speech.testTextRequired") : undefined
  const cancel = () => { controller.current?.abort(); capture.current?.cancel(); capture.current = undefined; setRecording(false); setBusy(false) }
  const cancelRef = useRef(cancel); cancelRef.current = cancel
  useEffect(() => () => { cancelRef.current(); if (speechPlayback.getSnapshot().key === "settings:speech") speechPlayback.stop() }, [])
  const finish = async () => {
    const active = capture.current
    const owner = controller.current
    if (!active || !owner) return
    capture.current = undefined; setRecording(false); setBusy(true)
    try {
      const blob = await active.stop()
      await onFlush()
      const recognized = await transcribeSpeech(blob, owner.signal)
      if (!owner.signal.aborted) setResult(recognized)
    } catch (error) {
      if (owner.signal.aborted) return
      const message = speechFailureMessage(error, t)
      if (message) toastError(message)
    }
    finally { if (controller.current === owner) setBusy(false) }
  }
  const start = async () => {
    const owner = new AbortController(); controller.current = owner; setBusy(true)
    try {
      const active = await startSpeechRecording({ signal: owner.signal, format: speech.input.recording_format, deviceId: speech.input.device_id,
        maxBytes: speech.limits.input_bytes, maxDurationSeconds: speech.input.max_duration_seconds, onLimit: () => { void finish() },
      })
      if (owner.signal.aborted) { active.cancel(); return }
      capture.current = active; setRecording(true)
    } catch (error) {
      if (owner.signal.aborted) return
      const message = speechFailureMessage(error, t)
      if (message) toastError(message)
    }
    finally { if (controller.current === owner) setBusy(false) }
  }
  return <SettingsSection description={t("speech.testHint")} title={t(kind === "input" ? "speech.testRecording" : "speech.testReading")}>
    {kind !== "output" ? <>
    <SettingsRow label={t("speech.testRecording")} hint={inputHint}>
      <div className="flex gap-2"><Button type="button" variant="outline" size="sm" aria-description={inputHint} disabled={Boolean(inputHint) || busy} onClick={() => void (recording ? finish() : start())}>{recording ? t("speech.finishRecording") : t("speech.testRecording")}</Button>
        {recording || busy ? <Button type="button" variant="ghost" size="sm" onClick={cancel}>{t("speech.cancel")}</Button> : null}</div>
    </SettingsRow>
    {result ? <Field label={t("speech.testResult")}><Textarea value={result} readOnly /></Field> : null}
    </> : null}
    {kind !== "input" ? <>
    <Field label={t("speech.testText")}><Textarea value={text} onChange={(event) => setText(event.target.value)} /></Field>
    <SettingsRow label={t("speech.testReading")} hint={outputHint}>
      <Button type="button" variant="outline" size="sm" aria-description={outputHint} disabled={Boolean(outputHint)} onClick={() => {
        if (playback.key === "settings:speech") { speechPlayback.stop(); return }
        void speechPlayback.play("settings:speech", text, { chunkChars: speech.limits.chunk_chars, synthesize: async (part, signal) => {
          await onFlush(); return synthesizeSpeech(part, signal, speech.limits.output_bytes)
        } }).catch((error) => {
          const message = speechFailureMessage(error, t)
          if (message) toastError(message)
        })
      }}>{playback.key === "settings:speech" ? t("speech.stopReading") : t("speech.testReading")}</Button>
      {playing ? <span role="status" aria-live="polite" className="text-xs text-muted-foreground">{t(playback.state === "loading" ? "speech.generating" : "speech.playing")}</span> : null}
    </SettingsRow>
    </> : null}
  </SettingsSection>
}
