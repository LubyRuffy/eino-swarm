import type { AudioOverrides, AudioParameterView } from "./audio-parameters"
/** Persisted on the PC; credentials never enter the phone snapshot. */
export interface SpeechConnectionSettings {
  enabled: boolean
  gateway_disabled?: boolean
  source: "account" | "provider" | "custom"
  provider_id: string
  base_url: string
  model: string
  api_key?: string
  has_api_key?: boolean
}

export interface SpeechInputSettings extends SpeechConnectionSettings {
  language: string
  prompt: string
  /** Browser audio input id. Empty follows the operating-system default. */
  device_id: string
  recording_format: "auto" | "wav"
  max_duration_seconds: number
  timeout_seconds: number
}

export interface SpeechOutputSettings extends SpeechConnectionSettings {
  voice: string
  speed: number
  format: "mp3" | "wav" | "pcm"
  instructions: string
  timeout_seconds: number
}

export interface SpeechLimits {
  input_bytes: number
  output_bytes: number
  chunk_chars: number
  staging_ttl_seconds: number
}

export interface SpeechSettings {
  gateway_parameters?: Record<string, AudioOverrides>
  input: SpeechInputSettings
  output: SpeechOutputSettings
  limits: SpeechLimits
}

export interface SpeechRoute {
  source_id?: string
  execution: "gateway" | "pc_local"
  gateway_origin?: string
}

export interface SpeechSnapshot extends SpeechRoute {
  input: Pick<SpeechInputSettings, "enabled" | "language" | "prompt" | "recording_format" | "max_duration_seconds" | "timeout_seconds"> & {
    device_id?: string
    user_disabled?: boolean
    ready: boolean; reason?: string; model: string; label: string; audio?: AudioParameterView
  } & Partial<SpeechRoute>
  output: Pick<SpeechOutputSettings, "enabled" | "voice" | "speed" | "format" | "instructions" | "timeout_seconds"> & {
    user_disabled?: boolean
    ready: boolean; reason?: string; model: string; label: string; audio?: AudioParameterView
  } & Partial<SpeechRoute>
  limits: SpeechLimits
}

/** Each direction owns its route. Only legacy snapshots without that
 * direction's authority fields inherit the global route. */
export function speechRoute(snapshot: SpeechSnapshot, kind: "input" | "output"): SpeechRoute | undefined {
  const direction = snapshot[kind]
  const route = direction.execution !== undefined || direction.gateway_origin !== undefined ? direction : snapshot
  if (route.execution === "pc_local" && (route === snapshot || route.gateway_origin === undefined)) return { execution: "pc_local" }
  if (route.execution === "gateway" && typeof route.gateway_origin === "string") return { execution: "gateway", gateway_origin: route.gateway_origin, source_id: direction.source_id }
}

export function defaultSpeechSettings(value?: Partial<SpeechSettings>): SpeechSettings {
  const connection = { enabled: false, source: "account" as const, provider_id: "", base_url: "", model: "" }
  return {
    gateway_parameters: value?.gateway_parameters,
    input: { ...connection, language: "", prompt: "", recording_format: "auto", max_duration_seconds: 120, timeout_seconds: 120, ...value?.input, device_id: value?.input?.device_id ?? "" },
    output: { ...connection, voice: "", speed: 1, format: "mp3", instructions: "", timeout_seconds: 120, ...value?.output },
    limits: { input_bytes: 25 << 20, output_bytes: 32 << 20, chunk_chars: 1000, staging_ttl_seconds: 300, ...value?.limits },
  }
}
