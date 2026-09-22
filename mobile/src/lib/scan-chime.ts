const RATE = 22050
const SECONDS = 0.18
const FREQ = 1760

let cachedSrc: string | undefined
let clip: HTMLAudioElement | undefined
let unlocking = false
let dingQueued = false

export function resetScanChime(): void {
  try {
    clip?.pause()
  } catch {
    // Some webviews throw if the clip never started.
  }
  clip = undefined
  unlocking = false
  dingQueued = false
}

function synthPcm(): Int16Array {
  const n = Math.floor(RATE * SECONDS)
  const pcm = new Int16Array(n)
  for (let i = 0; i < n; i++) {
    const time = i / RATE
    const env = Math.min(1, time / 0.008) * Math.exp(-time * 28)
    const tone = Math.sin(2 * Math.PI * FREQ * time) + 0.18 * Math.sin(2 * Math.PI * FREQ * 2 * time)
    const sample = Math.max(-1, Math.min(1, tone * env))
    pcm[i] = (sample * 0x7fff) | 0
  }
  return pcm
}

function wavBytes(pcm: Int16Array): Uint8Array {
  const dataSize = pcm.length * 2
  const bytes = new Uint8Array(44 + dataSize)
  const view = new DataView(bytes.buffer)
  const write = (offset: number, text: string) => {
    for (let i = 0; i < text.length; i++) bytes[offset + i] = text.charCodeAt(i)
  }
  write(0, "RIFF")
  view.setUint32(4, 36 + dataSize, true)
  write(8, "WAVE")
  write(12, "fmt ")
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 1, true)
  view.setUint32(24, RATE, true)
  view.setUint32(28, RATE * 2, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  write(36, "data")
  view.setUint32(40, dataSize, true)
  let offset = 44
  for (let i = 0; i < pcm.length; i++) {
    view.setInt16(offset, pcm[i], true)
    offset += 2
  }
  return bytes
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ""
  const chunk = 0x8000
  for (let i = 0; i < bytes.length; i += chunk) {
    const slice = bytes.subarray(i, Math.min(bytes.length, i + chunk))
    for (let j = 0; j < slice.length; j++) binary += String.fromCharCode(slice[j])
  }
  return btoa(binary)
}

export function scanChimeSrc(): string {
  if (!cachedSrc) cachedSrc = "data:audio/wav;base64," + bytesToBase64(wavBytes(synthPcm()))
  return cachedSrc
}

function ensureClip(): HTMLAudioElement | undefined {
  if (clip) return clip
  if (typeof Audio === "undefined") return undefined
  try {
    clip = new Audio(scanChimeSrc())
    clip.preload = "auto"
    return clip
  } catch {
    return undefined
  }
}

// iOS will not play a later ding unless this same element already started
// inside the tap that opened the scanner. The unlock play is silent; a code
// already in frame sets dingQueued so that pause cannot swallow the ding.
export function primeScanChime(): void {
  const audio = ensureClip()
  if (!audio || unlocking) return
  unlocking = true
  audio.volume = 0
  const finishUnlock = () => {
    if (dingQueued) {
      audio.volume = 1
      return
    }
    try {
      audio.pause()
      audio.currentTime = 0
    } catch {
      // The unlock only has to mark the element as played.
    }
    audio.volume = 1
  }
  try {
    const pending = audio.play()
    if (!pending || typeof pending.then !== "function") {
      finishUnlock()
      return
    }
    void pending.then(finishUnlock).catch(() => undefined)
  } catch {
    unlocking = false
    audio.volume = 1
  }
}

export function playScanChime(): void {
  const audio = ensureClip()
  if (!audio) return
  dingQueued = true
  audio.volume = 1
  try {
    audio.currentTime = 0
    const pending = audio.play()
    if (pending && typeof pending.catch === "function") void pending.catch(() => undefined)
  } catch {
    // Autoplay can still refuse; the bind itself must not fail on a sound.
  }
}
