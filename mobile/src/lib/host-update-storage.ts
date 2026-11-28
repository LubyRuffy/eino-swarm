import { sha256 } from "@noble/hashes/sha2.js"

import { bytesToHex, randomBytes, utf8 } from "./bytes"
import { hubEndpoint } from "./link-authority"
import type { SavedLink } from "./store"

export type PendingHostUpdate = { operation_id: string; current: string; version: string }
export const releaseVersionPattern = /^\d+\.\d+\.\d+$/
const operationPattern = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/

function storageKey(owner: SavedLink): string {
  const parts = [hubEndpoint(owner.hubURL), owner.fingerprint, owner.hostPub, owner.ticket, owner.sessionID,
    owner.gatewayOrigin, owner.gatewayOwnerID, owner.gatewayHostID, owner.gatewayMobileID]
  return "zwai.host-update." + bytesToHex(sha256(utf8(JSON.stringify(parts))))
}

export function newHostUpdateID(): string {
  const bytes = randomBytes(16)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = bytesToHex(bytes)
  return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join("-")
}

export function getPendingHostUpdate(owner: SavedLink): PendingHostUpdate | undefined {
  try {
    const raw = localStorage.getItem(storageKey(owner))
    if (!raw) return
    const { operation_id, current, version } = JSON.parse(raw) as Partial<PendingHostUpdate>
    if (typeof operation_id === "string" && operationPattern.test(operation_id) &&
      typeof current === "string" && releaseVersionPattern.test(current) &&
      typeof version === "string" && releaseVersionPattern.test(version)) return { operation_id, current, version }
  } catch {
    // A blocked or old storage entry does not affect the active paired link.
  }
}

export function savePendingHostUpdate(owner: SavedLink, { operation_id, current, version }: PendingHostUpdate) {
  try { localStorage.setItem(storageKey(owner), JSON.stringify({ operation_id, current, version })) } catch { /* in-memory observation remains available */ }
}

export function clearPendingHostUpdate(owner: SavedLink, operationID: string) {
  try {
    if (getPendingHostUpdate(owner)?.operation_id === operationID) localStorage.removeItem(storageKey(owner))
  } catch { /* storage can be disabled */ }
}
