import { LinkFault } from "./client"
import type { RemoteLink } from "./link"
import { OpHostUpdateCheck, OpHostUpdateStart, OpHostUpdateStatus, OpMeta, PROTOCOL_V,
  type HostUpdateProgress, type RemoteResponse,
} from "./rpc"
import type { SavedLink } from "./store"

const currentVersion = "1.2.3"
const targetVersion = "1.2.4"
const restartAt = 2400
const restoredAt = 6700
type Attempt = { id: string; started: number; challenge: string; verified: boolean }
const attempts = new Map<string, Attempt>()

/** Offline native and browser fixtures exercise the product controller using
 *  its real operations, including application-level reconnect handling. */
export function withMockHostUpdate(link: RemoteLink, saved: SavedLink): RemoteLink {
  const fixture = new URLSearchParams(location.search).get("host-update")
  if (fixture !== "1" && fixture !== "pending") return link
  const rpc = link.rpc.bind(link)
  link.rpc = async (req, opts) => {
    const attempt = attempts.get(saved.fingerprint)
    const elapsed = attempt ? Date.now() - attempt.started : -1
    const restarting = fixture === "1" && elapsed >= restartAt
    const version = restarting ? targetVersion : currentVersion
    const reply = (part: Partial<RemoteResponse>): RemoteResponse => ({ v: PROTOCOL_V, id: req.id ?? "", ok: true, ...part })
    if (req.op === OpMeta) {
      const result = await rpc(req, opts)
      return { ...result, host_update_supported: true, host_version: version,
        ...(restarting && (!attempt?.verified || elapsed < restoredAt) ? { startup: "restoring" } : {}),
      }
    }
    if (req.op === OpHostUpdateCheck) return reply({ host_update_offer: version === targetVersion
      ? { status: "current", current: version }
      : { status: "available", current: version, offer: { version: targetVersion } },
    })
    if (req.op === OpHostUpdateStart && req.version === targetVersion && req.operation_id) {
      if (!req.host_update_verify) return reply({ ok: false, code: "update_verification_required" })
      if (!attempt) {
        const challenge = req.operation_id.replaceAll("-", "").repeat(2)
        attempts.set(saved.fingerprint, { id: req.operation_id, started: Date.now(), challenge, verified: false })
        if (fixture === "1") window.setTimeout(() => {
          if (!link.alive()) return
          const disconnected = link.onDisconnect
          link.close()
          disconnected?.(new LinkFault("offline", "network"))
        }, restartAt)
      } else if (attempt.id !== req.operation_id) return reply({ ok: false, code: "update_busy" })
      return reply({ host_update: { operation_id: req.operation_id, version: targetVersion, stage: "checking" } })
    }
    if (req.op === OpHostUpdateStatus) {
      if (!attempt || attempt.id !== req.operation_id) return reply({ ok: false, code: "not_found" })
      if (fixture === "pending") return reply({ host_update: { operation_id: attempt.id, version: targetVersion,
        stage: "pending", error: "Waiting for the new process to publish its lease.",
      } })
      if (restarting && req.host_update_challenge === attempt.challenge) attempt.verified = true
      const stage: HostUpdateProgress["stage"] = elapsed >= restoredAt && attempt.verified ? "completed"
        : restarting ? "restoring" : "downloading"
      return reply({ host_version: version,
        ...(restarting && !attempt.verified ? { host_update_challenge: attempt.challenge } : {}),
        host_update: { operation_id: attempt.id, version: targetVersion, stage,
        ...(stage === "downloading" ? { downloaded_bytes: 2, total_bytes: 5 } : {}),
      } })
    }
    return rpc(req, opts)
  }
  return link
}
