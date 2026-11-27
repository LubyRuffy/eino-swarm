import { safeAudioOverrides } from "../../../frontend/src/lib/audio-parameters"
import { sha256 } from "@noble/hashes/sha2.js"
import type { SpeechSnapshot } from "../../../frontend/src/lib/speech-types"
import { bytesToHex, utf8 } from "./bytes"
import { normalizeGatewayOrigin } from "./gateway-account"
import type { SavedLink } from "./store"

const key = "zwai.phone.speech"
export type SyncedSpeech = { binding: string; syncedAt: number; snapshot: SpeechSnapshot }

/** A fingerprint alone survives a new pairing. Hash the complete authority so
 * a removed grant cannot silently inherit its predecessor's executable cache. */
export function speechBinding(pc: SavedLink): string {
  return bytesToHex(sha256(utf8(JSON.stringify([
    pc.fingerprint, pc.hostPub, pc.hubURL, pc.ticket, pc.sessionID,
    pc.gatewayOrigin, pc.gatewayOwnerID, pc.gatewayHostID, pc.gatewayMobileID,
  ]))))
}

const text = (value: unknown, max = 8192): string | undefined => typeof value === "string" && new TextEncoder().encode(value).length <= max ? value : undefined
const count = (value: unknown, maximum: number): value is number => typeof value === "number" && Number.isSafeInteger(value) && value > 0 && value <= maximum

/** Copy an explicit allowlist. Credentials, endpoint paths and the PC browser's
 * microphone id stay off the phone; the phone uses its own microphone. */
export function safeSpeechSnapshot(raw: unknown): SpeechSnapshot | undefined {
  if (!raw || typeof raw !== "object") return
  const source = raw as Record<string, unknown>
  if (source.execution !== "gateway" && source.execution !== "pc_local") return
  const input = source.input as Record<string, unknown> | undefined
  const output = source.output as Record<string, unknown> | undefined
  const limits = source.limits as Record<string, unknown> | undefined
  if (!input || !output || !limits) return
  if ([input.enabled, input.ready, output.enabled, output.ready].some((item) => typeof item !== "boolean")) return
  const gatewayInput = input.execution === "gateway" || input.execution === undefined && source.execution === "gateway"
  const gatewayOutput = output.execution === "gateway" || output.execution === undefined && source.execution === "gateway"
  for (const [item, maximum] of [[input.model, 256], [output.model, 256], [input.label, 8192], [output.label, 8192], [input.language, 256], [input.prompt, gatewayInput && input.audio ? 400000 : 8192], [output.voice, 256], [output.instructions, gatewayOutput && output.audio ? 400000 : 8192]] as const) if (text(item, maximum) === undefined) return
  if (!["auto", "wav"].includes(input.recording_format as string) || !["mp3", "wav", "pcm"].includes(output.format as string)) return
  if (!count(input.max_duration_seconds, 3600) || !count(input.timeout_seconds, 3600) || !count(output.timeout_seconds, 3600)) return
  if (typeof output.speed !== "number" || !Number.isFinite(output.speed) || output.speed <= 0 || !gatewayOutput && (output.speed < 0.25 || output.speed > 4)) return
  if (!count(limits.input_bytes, 256 * 1024 * 1024) || !count(limits.output_bytes, 256 * 1024 * 1024) || !count(limits.chunk_chars, 4096) || !count(limits.staging_ttl_seconds, 3600)) return
  let outputRoute: Pick<SpeechSnapshot["output"], "execution" | "gateway_origin"> = {}
  let inputRoute: Pick<SpeechSnapshot["input"], "execution" | "gateway_origin"> = {}
  if (input.execution !== undefined || input.gateway_origin !== undefined) {
    if (input.execution === "gateway") {
      if (typeof input.gateway_origin !== "string") return
      try { inputRoute = { execution: "gateway", gateway_origin: normalizeGatewayOrigin(input.gateway_origin) } } catch { return }
    } else if (input.execution === "pc_local" && input.gateway_origin === undefined) inputRoute = { execution: "pc_local" }
    else return
  }
  if (output.execution !== undefined || output.gateway_origin !== undefined) {
    if (output.execution === "gateway") {
      if (typeof output.gateway_origin !== "string") return
      try { outputRoute = { execution: "gateway", gateway_origin: normalizeGatewayOrigin(output.gateway_origin) } } catch { return }
    } else if (output.execution === "pc_local" && output.gateway_origin === undefined) outputRoute = { execution: "pc_local" }
    else return
  }
  let origin: string | undefined
  if (source.execution === "gateway") {
    if (source.gateway_origin) { try { origin = normalizeGatewayOrigin(String(source.gateway_origin)) } catch { return } }
    else if ((input.execution === undefined && input.enabled && input.ready) || (output.execution === undefined && output.enabled && output.ready)) return
  }
  return {
    execution: source.execution, ...(origin ? { gateway_origin: origin } : {}),
    input: {
      ...inputRoute,
      ...(gatewayInput && input.audio && typeof input.audio === "object" ? { audio: {version:0,status:"unsupported",scope:"",overrides:safeAudioOverrides((input.audio as Record<string,unknown>).overrides)} } : {}),
      ...(text(input.source_id, 128) ? { source_id: text(input.source_id, 128) } : {}),
      enabled: input.enabled as boolean, ready: input.ready as boolean,
      ...(typeof input.user_disabled === "boolean" ? { user_disabled: input.user_disabled } : {}),
      ...(text(input.reason, 256) ? { reason: text(input.reason, 256) } : {}),
      model: input.model as string, label: input.label as string, language: input.language as string, prompt: input.prompt as string,
      recording_format: input.recording_format as "auto" | "wav", max_duration_seconds: input.max_duration_seconds, timeout_seconds: input.timeout_seconds,
    },
    output: {
      ...outputRoute,
      ...(gatewayOutput && output.audio && typeof output.audio === "object" ? { audio: {version:0,status:"unsupported",scope:"",overrides:safeAudioOverrides((output.audio as Record<string,unknown>).overrides)} } : {}),
      ...(text(output.source_id, 128) ? { source_id: text(output.source_id, 128) } : {}),
      enabled: output.enabled as boolean, ready: output.ready as boolean,
      ...(typeof output.user_disabled === "boolean" ? { user_disabled: output.user_disabled } : {}),
      ...(text(output.reason, 256) ? { reason: text(output.reason, 256) } : {}),
      model: output.model as string, label: output.label as string, voice: output.voice as string,
      speed: output.speed, format: output.format as "mp3" | "wav" | "pcm", instructions: output.instructions as string, timeout_seconds: output.timeout_seconds,
    },
    limits: { input_bytes: limits.input_bytes, output_bytes: limits.output_bytes, chunk_chars: limits.chunk_chars, staging_ttl_seconds: limits.staging_ttl_seconds },
  }
}

function stored(): Record<string, SyncedSpeech> {
  try { const raw: unknown = JSON.parse(localStorage.getItem(key) || "{}"); return raw && typeof raw === "object" && !Array.isArray(raw) ? raw as Record<string, SyncedSpeech> : {} } catch { return {} }
}

export function loadSpeechCache(pc: SavedLink): SyncedSpeech | undefined {
  const row = stored()[pc.fingerprint]
  if (!row || row.binding !== speechBinding(pc) || !Number.isFinite(row.syncedAt)) return
  const snapshot = safeSpeechSnapshot(row.snapshot)
  return snapshot ? { binding: row.binding, syncedAt: row.syncedAt, snapshot } : undefined
}

export function saveSpeechCache(pc: SavedLink, raw: unknown, at = Date.now()): SyncedSpeech | undefined {
  const snapshot = safeSpeechSnapshot(raw)
  if (!snapshot) return
  const row = { binding: speechBinding(pc), syncedAt: at, snapshot }
  try { localStorage.setItem(key, JSON.stringify({ ...stored(), [pc.fingerprint]: row })) } catch { /* The in-memory synchronized configuration remains usable. */ }
  return row
}

/** A live rejection supersedes the offline cache for this exact grant only. */
export function forgetSpeechCache(pc: SavedLink): void {
  const rows = stored()
  if (rows[pc.fingerprint]?.binding !== speechBinding(pc)) return
  delete rows[pc.fingerprint]
  try { if (Object.keys(rows).length) localStorage.setItem(key, JSON.stringify(rows)); else localStorage.removeItem(key) } catch { /* The live owner still revokes its in-memory snapshot. */ }
}

export function pruneSpeechCaches(pcs: SavedLink[]): void {
  const rows = stored()
  for (const [fingerprint, row] of Object.entries(rows)) if (!row || typeof row !== "object" || !pcs.some((pc) => pc.fingerprint === fingerprint && row.binding === speechBinding(pc))) delete rows[fingerprint]
  try { if (Object.keys(rows).length) localStorage.setItem(key, JSON.stringify(rows)); else localStorage.removeItem(key) } catch { /* Storage can be unavailable; no fallback authority is selected. */ }
}
