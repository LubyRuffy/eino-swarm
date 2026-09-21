import type { RemoteBinding } from "./types"

/** Title is the phone-reported model. Fingerprint is the fallback, not the product. */
export function bindingTitle(b: RemoteBinding): string {
  const label = b.device?.trim()
  return label || b.device_fp
}

export function bindingFingerprint(b: RemoteBinding): string {
  return b.device?.trim() ? b.device_fp : ""
}

export function bindingWhen(b: RemoteBinding): string {
  return b.last_seen?.trim() || b.created_at
}

export function bindingUsesLastSeen(b: RemoteBinding): boolean {
  return Boolean(b.last_seen?.trim())
}
