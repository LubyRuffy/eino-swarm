import type { GatewayHost, GatewayUser } from "./gateway-account"
import { t } from "./i18n"

function cleanName(value: unknown): string {
  return typeof value === "string" ? value.trim() : ""
}

export function gatewayUserDisplayName(user: GatewayUser): string {
  const display = cleanName(user.display_name)
  if (display) return display
  const username = cleanName(user.username)
  // Older Gateways used the sanitized CAS label inside a generated login name.
  const legacy = /^nosec_([\p{L}\p{Nd}_-]{0,24})_[a-f0-9]{16}$/u.exec(username)
  if (legacy) return /[\p{L}\p{Nd}]/u.test(legacy[1]) ? legacy[1] : t("account.unnamedUser")
  return username && username !== user.id && !/^[a-f0-9]{32}$/i.test(username) ? username : t("account.unnamedUser")
}

export function readableGatewayPCName(value: unknown, id?: string, fingerprint?: string): string | undefined {
  const name = cleanName(value)
  if (!name || name === id || name === fingerprint || /^(?:open-[a-f0-9]{16}|[a-f0-9]{32})$/i.test(name)) return undefined
  return name
}

export function gatewayHostDisplayName(host: GatewayHost): string | undefined {
  return readableGatewayPCName(host.display_name, host.id, host.host_fp) ||
    readableGatewayPCName(host.name, host.id, host.host_fp) ||
    readableGatewayPCName(host.hostname, host.id, host.host_fp)
}
