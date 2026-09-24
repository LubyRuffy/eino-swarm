import { Capacitor } from "@capacitor/core"

/** Settings only paints one line. A UA is not a biography. */
export const DEVICE_LABEL_MAX = 80

export type DeviceLabelInput = {
  platform?: string
  userAgent?: string
}

/** Name and model stay apart. A name that happens to contain a model is still
 *  just the name; the model comes from the UA slot, not from splitting that line. */
export function deviceFacts(input: DeviceLabelInput = {}): { name: string; model: string } {
  const platform = (input.platform ?? detectPlatform()).trim().toLowerCase()
  const ua = input.userAgent ?? (typeof navigator !== "undefined" ? navigator.userAgent : "")
  const model = deviceModel(platform, ua)
  const parts = [osName(platform, ua), osVersion(platform, ua), model].filter(Boolean)
  return {
    name: clipDeviceLabel(parts.join(" ") || fallbackLabel(platform)),
    model: clipDeviceLabel(model),
  }
}

export function deviceLabel(input: DeviceLabelInput = {}): string {
  return deviceFacts(input).name
}

/** Hub label fields. Empty model stays empty; callers must not invent one from the name. */
export function hubDeviceFields(version = "", input: DeviceLabelInput = {}) {
  const facts = deviceFacts(input)
  return { name: facts.name, model: facts.model, version }
}

export function clipDeviceLabel(raw: string): string {
  const collapsed = raw.replace(/[\u0000-\u001f]+/g, " ").replace(/\s+/g, " ").trim()
  if ([...collapsed].length <= DEVICE_LABEL_MAX) return collapsed
  return [...collapsed].slice(0, DEVICE_LABEL_MAX).join("")
}

function detectPlatform(): string {
  try {
    return Capacitor.getPlatform()
  } catch {
    return "web"
  }
}

function osName(platform: string, ua: string): string {
  if (platform === "ios" || /iPhone|iPad|iPod/.test(ua)) return "iOS"
  if (platform === "android" || /Android/i.test(ua)) return "Android"
  if (platform === "web") return "Web"
  return platform
}

function osVersion(platform: string, ua: string): string {
  if (platform === "ios" || /iPhone|iPad|iPod/.test(ua)) {
    const m = ua.match(/OS (\d+)[._](\d+)(?:[._](\d+))?/)
    if (m) return [m[1], m[2], m[3]].filter(Boolean).join(".")
  }
  if (platform === "android" || /Android/i.test(ua)) {
    const m = ua.match(/Android (\d+(?:\.\d+)*)/i)
    if (m) return m[1]
  }
  return ""
}

function deviceModel(platform: string, ua: string): string {
  if (/iPad/.test(ua)) return "iPad"
  if (/iPod/.test(ua)) return "iPod"
  if (/iPhone/.test(ua) || platform === "ios") return "iPhone"
  if (platform === "android" || /Android/i.test(ua)) {
    const m = ua.match(/Android [^;]+;\s*([^);]+)/i)
    if (!m) return ""
    const model = m[1].replace(/\s+Build\/.*$/i, "").trim()
    if (!model || /^(wv|Mobile)$/i.test(model)) return ""
    return model
  }
  return ""
}

function fallbackLabel(platform: string): string {
  if (platform === "ios") return "iOS"
  if (platform === "android") return "Android"
  return "Web"
}
