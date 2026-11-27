import { speechRoute, type SpeechSnapshot } from "../../../frontend/src/lib/speech-types"

export type VoiceOwner = "gateway" | "pc" | "phone"
/** One direction may use a PC override while the other inherits the Gateway. */
export function selectPhoneSpeech(gateway: SpeechSnapshot | undefined, preferred: SpeechSnapshot | undefined, personal: boolean, connected: boolean, gatewayAllowed: (kind: "input" | "output") => boolean) {
  const base = gateway ?? preferred
  if (!base) return { snapshot: undefined, owners: {} as Partial<Record<"input" | "output", VoiceOwner>> }
  const snapshot: SpeechSnapshot = { ...base, input: { ...base.input }, output: { ...base.output }, limits: { ...base.limits } }
  const owners: Partial<Record<"input" | "output", VoiceOwner>> = {}
  for (const kind of ["input", "output"] as const) {
    const slot = preferred?.[kind]
    if (!personal && slot?.user_disabled) {
      Object.assign(snapshot[kind], { enabled: false, ready: false })
      continue
    }
    const route = preferred && speechRoute(preferred, kind)
    const custom = slot?.enabled && slot.model && (personal || route?.execution === "pc_local")
    const pcGateway = slot?.enabled && (slot.ready || slot.audio !== undefined) && slot.model && connected && route?.execution === "gateway" && gatewayAllowed(kind)
    const usePreferred = Boolean(custom || pcGateway)
    const selected = usePreferred ? slot : gateway?.[kind]
    const selectedRoute = usePreferred ? route : gateway && speechRoute(gateway, kind)
    if (!selected) {
      Object.assign(snapshot[kind], { enabled: false, ready: false })
      continue
    }
    const owner: VoiceOwner = custom ? personal ? "phone" : "pc" : "gateway"
    owners[kind] = owner
    const ready = selected.ready && (owner !== "pc" || connected)
    if (kind === "input") snapshot.input = { ...selected as SpeechSnapshot["input"], ...selectedRoute, ready }
    else snapshot.output = { ...selected as SpeechSnapshot["output"], ...selectedRoute, ready }
    const limits = usePreferred ? preferred!.limits : gateway!.limits
    if (kind === "input") snapshot.limits.input_bytes = limits.input_bytes
    else { snapshot.limits.output_bytes = limits.output_bytes; snapshot.limits.chunk_chars = limits.chunk_chars }
    if (owner === "pc") snapshot.limits.staging_ttl_seconds = limits.staging_ttl_seconds
  }
  return { snapshot, owners }
}

/** Ignore timestamps, labels and object insertion order when deciding whether
 * a live utterance has lost its parameters or authority. */
export function speechExecutionKey(snapshot: SpeechSnapshot | undefined, owners: Partial<Record<"input" | "output", VoiceOwner>>): string {
  if (!snapshot) return ""
  const slot = (kind: "input" | "output") => {
    const value = snapshot[kind], route = speechRoute(snapshot, kind)
    return [owners[kind], route?.execution, route?.gateway_origin, route?.source_id, value.enabled, value.ready, value.model, value.timeout_seconds,value.audio]
  }
  return JSON.stringify([slot("input"), slot("output"), snapshot.input.language, snapshot.input.prompt, snapshot.input.recording_format, snapshot.input.max_duration_seconds,
    snapshot.output.voice, snapshot.output.speed, snapshot.output.format, snapshot.output.instructions, snapshot.limits])
}
