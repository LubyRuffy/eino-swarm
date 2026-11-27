import { GATEWAY_AUDIO_PREFERENCES_EVENT } from "./gateway-audio-parameters"
import { selectPhoneSpeech, speechExecutionKey, type VoiceOwner } from "./phone-speech-selection"
import { authoritativeGatewayAudio, currentGatewayVoice, gatewayCapabilityAllows, GATEWAY_CAPABILITIES_EVENT } from "./gateway-capabilities"
import { useEffect, useSyncExternalStore } from "react"
import { speechRoute, type SpeechSnapshot } from "../../../frontend/src/lib/speech-types"
import { speechPlayback } from "../../../frontend/src/lib/speech-media"
import { transcriptionText } from "../../../frontend/src/lib/speech-text"
import type { SpeechAudio } from "../../../frontend/src/lib/speech-stream"
import { GATEWAY_ACCOUNT_EVENT, loadGatewaySession } from "./gateway-account"
import { gatewayAuthorityCurrent, sameGatewayAuthority, GATEWAY_REFUSAL_EVENT } from "./gateway-authority"
import type { RemoteLink } from "./link"
import { OpSpeechConfig } from "./rpc"
import { forgetSpeechCache, loadSpeechCache, pruneSpeechCaches, saveSpeechCache, speechBinding, type SyncedSpeech } from "./speech-cache"
import { gatewaySpeechCall, localSpeechCall, retainSpeechStream, SpeechCallError } from "./speech-transport"
import type { SavedLink } from "./store"
import { LOCAL_SPEECH_EVENT, localPhoneSpeechCall, localSpeechSnapshot } from "./local-speech"

export type PhoneSpeechState = {
  epoch: number
  snapshot?: SpeechSnapshot
  syncedAt?: number
  host?: string
  connected: boolean
  inputReady: boolean
  outputReady: boolean
  standalone?: boolean
}
let state: PhoneSpeechState = { epoch: 0, connected: false, inputReady: false, outputReady: false }
let source: { pc: SavedLink; link: RemoteLink | null; cached?: SyncedSpeech } | undefined
let standalone = false
const listeners = new Set<() => void>()
const requests = new Set<AbortController>()
const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener) } }
const snapshot = () => state

export function cancelPhoneSpeech(): void {
  for (const request of requests) request.abort()
  speechPlayback.stop()
  window.dispatchEvent(new Event("zwai.phone.speech.cancel"))
}

let owners: Partial<Record<"input" | "output", VoiceOwner>> = {}
let executionKey = ""
let pcOwner: RemoteLink | null | undefined
let gatewayOwner = ""

function publish(forceCancel: boolean) {
  const session = loadGatewaySession()
  const authorized = Boolean(session && gatewayAuthorityCurrent(session))
  const connected = Boolean(source?.link?.alive())
  const preferred = standalone ? localSpeechSnapshot() : source?.cached?.snapshot
  const gateway = session && authorized ? currentGatewayVoice(session) : undefined
  const gatewayAllowed = (kind: "input" | "output") => {
    const route = preferred && speechRoute(preferred, kind)
    return Boolean(route?.execution === "gateway" && session && authorized && route.gateway_origin === session.origin &&
      gatewayCapabilityAllows(session, route.source_id, kind === "input" ? "transcription" : "speech") &&
      (!source?.pc.gatewayOrigin || source.pc.gatewayOrigin === session.origin) &&
      (!source?.pc.gatewayOwnerID || source.pc.gatewayOwnerID === session.user.id) &&
      (!source?.pc.gatewayMobileID || source.pc.gatewayMobileID === session.device.id))
  }
  const chosen = selectPhoneSpeech(gateway, preferred, standalone, connected, gatewayAllowed)
  owners = chosen.owners
  const speech = session && chosen.snapshot ? authoritativeGatewayAudio(session,chosen.snapshot) ?? chosen.snapshot : chosen.snapshot
  const key = speechExecutionKey(speech, owners)
  const usesPC = Object.values(owners).includes("pc")
  const usesGateway = Object.values(owners).includes("gateway")
  const nextPC = usesPC ? source?.link : undefined
  const nextGateway = usesGateway && session ? JSON.stringify([session.origin, session.token, session.user.id, session.device.id]) : ""
  const changed = forceCancel || executionKey !== key || pcOwner !== nextPC || gatewayOwner !== nextGateway
  executionKey = key; pcOwner = nextPC; gatewayOwner = nextGateway
  if (changed) cancelPhoneSpeech()
  state = { epoch: state.epoch + (changed ? 1 : 0), snapshot: speech,
    syncedAt: usesPC ? source?.cached?.syncedAt : undefined, host: usesPC ? source?.pc.label || source?.pc.fingerprint : undefined,
    connected, standalone, inputReady: Boolean(speech?.input.enabled && speech.input.ready), outputReady: Boolean(speech?.output.enabled && speech.output.ready) }
  for (const listener of listeners) listener()
}

/** Personal PC overrides require that PC. Gateway defaults use only this
 * phone's own account capability and remain independent of PC voice setup. */
export function setPhoneSpeechSource(pc: SavedLink | undefined, link: RemoteLink | null): void {
  if (pc && source && speechBinding(pc) === speechBinding(source.pc)) {
    // A PC disconnect retires its overrides; Gateway defaults own no PC socket.
    source.pc = pc
    source.link = link
    publish(false)
    return
  }
  source = pc ? { pc, link, cached: loadSpeechCache(pc) } : undefined
  publish(true)
}

export function acceptPhoneSpeechConfig(pc: SavedLink, raw: unknown): boolean {
  if (!source || speechBinding(source.pc) !== speechBinding(pc)) return false
  const cached = saveSpeechCache(pc, raw)
  if (!cached) {
    forgetSpeechCache(pc)
    source.cached = undefined
    publish(false)
    return false
  }
  source.cached = cached
  publish(false)
  return true
}

export function usePhoneSpeechSync(link: RemoteLink | null, pc: SavedLink | undefined, hosts: SavedLink[], local = false, providers?: unknown): void {
  const binding = pc ? speechBinding(pc) : ""
  // Phone model catalogs refresh independently of the selected PC. Only a
  // standalone phone binding may retire voice when those providers change.
  const localRevision=local ? JSON.stringify(providers) : ""
  useEffect(()=>{
    standalone=local;publish(true)
    const refresh=()=>publish(true)
    window.addEventListener(LOCAL_SPEECH_EVENT,refresh)
    return ()=>{standalone=false;window.removeEventListener(LOCAL_SPEECH_EVENT,refresh);publish(true)}
  },[local,localRevision])
  useEffect(() => { pruneSpeechCaches(hosts) }, [hosts])
  // Retire the authority on PC/grant changes or unmount. The link effect below
  // also cancels owned audio requests when the selected transport changes.
  useEffect(() => () => {
    if (source && speechBinding(source.pc) === binding) { source = undefined; publish(true) }
  }, [binding])
  useEffect(() => {
    setPhoneSpeechSource(pc, link)
    let alive = true
    let busy = false
    const refresh = async () => {
      if (!pc || !link?.alive() || busy || document.hidden) { publish(false); return }
      busy = true
      try {
        const response = await link.rpc({ op: OpSpeechConfig }, { dropOnTimeout: false, timeoutMs: 15000 })
        if (!alive || !source || speechBinding(source.pc) !== binding || source.link !== link) return
        if (response.ok && response.speech) acceptPhoneSpeechConfig(pc, response.speech)
        else if (response.ok || response.code === "unknown_op" || response.code === "audio_config_limit") {
          // A live old host cannot authorize cached features it no longer declares.
          source.cached = undefined
          forgetSpeechCache(pc)
          publish(false)
        }
      } catch { if (alive) publish(false) }
      finally { busy = false }
    }
    let authority = loadGatewaySession()
    let authorityAllowed = Boolean(authority && gatewayAuthorityCurrent(authority))
    const authorityChanged = () => {
      const next = loadGatewaySession()
      const nextAllowed = Boolean(next && gatewayAuthorityCurrent(next))
      // Health refreshes also publish account events. A new avatar or renewed
      // expiry is not a new credential and must not interrupt dictation.
      const changed = Boolean(authority) !== Boolean(next) ||
        Boolean(authority && next && !sameGatewayAuthority(authority, next)) || authorityAllowed !== nextAllowed
      authority = next
      authorityAllowed = nextAllowed
      publish(changed)
    }
    const authorityRefused = () => publish(false)
    const visibility = () => { if (document.hidden) cancelPhoneSpeech(); else void refresh() }
    const timer = setInterval(() => { void refresh() }, 15000)
    window.addEventListener("focus", refresh)
    document.addEventListener("visibilitychange", visibility)
    window.addEventListener(GATEWAY_ACCOUNT_EVENT, authorityChanged)
    window.addEventListener(GATEWAY_REFUSAL_EVENT, authorityRefused)
    window.addEventListener(GATEWAY_CAPABILITIES_EVENT, authorityRefused)
    window.addEventListener(GATEWAY_AUDIO_PREFERENCES_EVENT, authorityRefused)
    void refresh()
    return () => {
      alive = false; clearInterval(timer)
      window.removeEventListener("focus", refresh)
      document.removeEventListener("visibilitychange", visibility)
      window.removeEventListener(GATEWAY_ACCOUNT_EVENT, authorityChanged)
      window.removeEventListener(GATEWAY_REFUSAL_EVENT, authorityRefused)
      window.removeEventListener(GATEWAY_CAPABILITIES_EVENT, authorityRefused)
      window.removeEventListener(GATEWAY_AUDIO_PREFERENCES_EVENT, authorityRefused)
    }
    // The complete binding captures every pairing authority field. Host labels
    // are presentation only, and the cached snapshot owns its own timestamp.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [link, binding])
}

export function usePhoneSpeech(): PhoneSpeechState { return useSyncExternalStore(subscribe, snapshot, snapshot) }

async function call(operation: "transcribe" | "speak", input: Blob | string, signal: AbortSignal): Promise<string | SpeechAudio> {
  const target = source
  const speech = state.snapshot
  const kind = operation === "transcribe" ? "input" : "output"
  const owner = owners[kind]
  const epoch = state.epoch
  if (!speech || !owner || !(kind === "input" ? state.inputReady : state.outputReady)) throw new SpeechCallError("unavailable")
  const controller = new AbortController()
  const abort = () => controller.abort()
  requests.add(controller)
  if (signal.aborted) abort(); else signal.addEventListener("abort", abort, { once: true })
  let retained = false
  const release = () => { signal.removeEventListener("abort", abort); requests.delete(controller); abort() }
  const check = () => {
    if (controller.signal.aborted || state.epoch !== epoch || (owner === "pc" && (source?.link !== target?.link || !target?.link?.alive()))) throw new DOMException("Aborted", "AbortError")
  }
  try {
    let result: string | SpeechAudio
    if (owner === "phone") {
      result = await localPhoneSpeechCall(operation, input, controller.signal)
    } else if (owner === "gateway") {
      const pairedLink = target?.link
      if (target && pairedLink?.alive() && typeof target.cached?.snapshot[kind].user_disabled === "boolean") {
        const response = await pairedLink.rpc({ op: OpSpeechConfig }, { dropOnTimeout: false, timeoutMs: 5000, signal: controller.signal })
        check()
        if (source?.link !== pairedLink || !pairedLink.alive() || !response.ok || !response.speech ||
            !acceptPhoneSpeechConfig(target.pc, response.speech)) throw new SpeechCallError("unavailable")
        check()
      }
      const session = loadGatewaySession()
      if (!session) throw new SpeechCallError("unavailable")
      result = await gatewaySpeechCall(speech!, session, operation, input, controller.signal)
    } else {
      if (!target?.link?.alive()) throw new SpeechCallError("unavailable")
      result = await localSpeechCall(speech!, target.link, operation, input, controller.signal)
    }
    check()
    if (typeof result !== "string" && "chunks" in result) {
      retained = true
      return retainSpeechStream(result, release, check, undefined, controller.signal)
    }
    return result
  } finally { if (!retained) release() }
}

export async function transcribePhoneSpeech(blob: Blob, signal: AbortSignal): Promise<string> {
  const text = transcriptionText(await call("transcribe", blob, signal) as string)
  if (!text.trim()) throw new SpeechCallError("empty")
  return text
}
export async function synthesizePhoneSpeech(text: string, signal: AbortSignal): Promise<SpeechAudio> { return await call("speak", text, signal) as SpeechAudio }
