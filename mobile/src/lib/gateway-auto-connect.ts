import type { GatewayHost } from "./gateway-account"
import type { SavedLink } from "./store"

/** The one computer to open right after a sign-in, or undefined when the
 *  person should choose. It applies only to a phone with no computer yet, and
 *  only when the account has exactly one that is online and enabled: with
 *  several, picking one for the user would be a guess. */
export function autoConnectTarget(
  hosts: GatewayHost[],
  saved: SavedLink[],
  activeFingerprint: string,
): GatewayHost | undefined {
  if (saved.length > 0 || activeFingerprint) return undefined
  const online = hosts.filter((host) => host.online && host.enabled)
  return online.length === 1 ? online[0] : undefined
}
