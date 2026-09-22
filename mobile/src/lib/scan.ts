import { Camera } from "@capacitor/camera"
import { Capacitor } from "@capacitor/core"
import jsQR from "jsqr"

import { t } from "./i18n"
import { parseOffer } from "./offer"

// A full-resolution frame makes jsQR stall the UI thread. The preview stays full size.
const DECODE_EDGE = 640
const DECODE_INTERVAL_MS = 140

export type ScanCameraReason = "denied" | "missing" | "failed"

export class ScanCameraError extends Error {
  readonly reason: ScanCameraReason

  constructor(reason: ScanCameraReason) {
    super(reason)
    this.name = "ScanCameraError"
    this.reason = reason
  }
}

export function frameSize(
  width: number,
  height: number,
  maxEdge = DECODE_EDGE,
): { width: number; height: number } {
  if (width <= 0 || height <= 0 || maxEdge <= 0) return { width: 0, height: 0 }
  const edge = Math.max(width, height)
  const scale = edge > maxEdge ? maxEdge / edge : 1
  return {
    width: Math.max(1, Math.round(width * scale)),
    height: Math.max(1, Math.round(height * scale)),
  }
}

export function payloadFromRGBA(
  data: Uint8ClampedArray,
  width: number,
  height: number,
): string | null {
  if (width <= 0 || height <= 0 || data.length < width * height * 4) return null
  // The pairing plate is dark-on-light. Trying both inversions halves the frame rate.
  const code = jsQR(data, width, height, { inversionAttempts: "dontInvert" })
  const text = code?.data?.trim()
  return text ? text : null
}

export function pairlinkFromQR(text: string): string | null {
  const trimmed = text.trim()
  if (!trimmed) return null
  try {
    parseOffer(trimmed)
  } catch {
    return null
  }
  return trimmed
}

export function scanCameraMessage(err: unknown): string {
  const reason = err instanceof ScanCameraError ? err.reason : "failed"
  if (reason === "denied") return t("scan.cameraDenied")
  if (reason === "missing") return t("scan.noCamera")
  return t("scan.cameraFailed")
}

export function releaseStream(stream: MediaStream): void {
  for (const track of stream.getTracks()) track.stop()
}

export function sampleFrame(
  video: HTMLVideoElement,
  canvas: HTMLCanvasElement,
  maxEdge = DECODE_EDGE,
): ImageData | null {
  const size = frameSize(video.videoWidth, video.videoHeight, maxEdge)
  if (size.width === 0 || size.height === 0) return null
  canvas.width = size.width
  canvas.height = size.height
  const ctx = canvas.getContext("2d", { willReadFrequently: true })
  if (!ctx) return null
  ctx.drawImage(video, 0, 0, size.width, size.height)
  return ctx.getImageData(0, 0, size.width, size.height)
}

export function startDecodeLoop(opts: {
  sample: () => string | null
  onText: (text: string) => void
  intervalMs?: number
}): { stop: () => void } {
  const interval = opts.intervalMs ?? DECODE_INTERVAL_MS
  let timer = 0
  let stopped = false
  const tick = () => {
    if (stopped) return
    let text: string | null = null
    try {
      text = opts.sample()
    } catch {
      text = null
    }
    if (text) {
      stopped = true
      opts.onText(text)
      return
    }
    timer = window.setTimeout(tick, interval)
  }
  timer = window.setTimeout(tick, interval)
  return {
    stop: () => {
      stopped = true
      window.clearTimeout(timer)
    },
  }
}

function cameraError(err: unknown): ScanCameraError {
  if (err instanceof ScanCameraError) return err
  if (err instanceof DOMException) {
    if (
      err.name === "NotAllowedError" ||
      err.name === "SecurityError" ||
      err.name === "PermissionDeniedError"
    ) {
      return new ScanCameraError("denied")
    }
    if (
      err.name === "NotFoundError" ||
      err.name === "DevicesNotFoundError" ||
      err.name === "OverconstrainedError" ||
      err.name === "NotReadableError"
    ) {
      return new ScanCameraError("missing")
    }
  }
  return new ScanCameraError("failed")
}

async function ensureNativeCameraPermission(): Promise<void> {
  if (!Capacitor.isNativePlatform()) return
  const status = await Camera.requestPermissions({ permissions: ["camera"] })
  if (status.camera === "denied") throw new ScanCameraError("denied")
}

function userMedia(constraints: MediaStreamConstraints): Promise<MediaStream> {
  const devices = navigator.mediaDevices
  if (!devices?.getUserMedia) return Promise.reject(new ScanCameraError("missing"))
  return devices.getUserMedia(constraints)
}

export async function openRearCamera(): Promise<MediaStream> {
  await ensureNativeCameraPermission()
  try {
    // ideal, not exact: a phone with only a front camera still previews.
    return await userMedia({
      audio: false,
      video: { facingMode: { ideal: "environment" } },
    })
  } catch (err) {
    if (err instanceof ScanCameraError) throw err
    if (
      err instanceof DOMException &&
      (err.name === "OverconstrainedError" || err.name === "NotFoundError")
    ) {
      try {
        return await userMedia({ audio: false, video: true })
      } catch (fallback) {
        throw cameraError(fallback)
      }
    }
    throw cameraError(err)
  }
}

export async function attachStream(video: HTMLVideoElement, stream: MediaStream): Promise<void> {
  video.srcObject = stream
  video.muted = true
  // Without playsinline, iOS lifts the element into the system player and the frame disappears.
  video.playsInline = true
  video.setAttribute("playsinline", "true")
  await video.play()
}

export async function startLiveScan(opts: {
  video: HTMLVideoElement
  onText: (text: string) => void
}): Promise<{ stop: () => void }> {
  const stream = await openRearCamera()
  let stopped = false
  let loop: { stop: () => void } | undefined
  const stop = () => {
    if (stopped) return
    stopped = true
    loop?.stop()
    releaseStream(stream)
    if (opts.video.srcObject === stream) opts.video.srcObject = null
  }
  try {
    await attachStream(opts.video, stream)
  } catch (err) {
    stop()
    throw cameraError(err)
  }
  if (stopped) return { stop }
  const canvas = document.createElement("canvas")
  loop = startDecodeLoop({
    sample: () => {
      const frame = sampleFrame(opts.video, canvas)
      if (!frame) return null
      return payloadFromRGBA(frame.data, frame.width, frame.height)
    },
    onText: (text) => {
      if (stopped) return
      opts.onText(text)
    },
  })
  return { stop }
}
