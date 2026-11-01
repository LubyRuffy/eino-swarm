import { GATEWAY_ACCOUNT_EVENT, loadGatewaySession, normalizeGatewayOrigin, type GatewaySession } from "./gateway-account"

export type WebExecution = "auto" | "off" | "cloud"
export type PcWebExecution = "inherit" | "local" | "cloud"
export const WEB_EXECUTION_EVENT = "zwai.phone.web_execution"
const legacyPrefix = "zwai.phone.webExecution:"
const directPrefix = "zwai.phone.directWebExecution:"
const pcPrefix = "zwai.phone.pcWebExecution:"

function scope(session: GatewaySession | null): string {
  return session ? normalizeGatewayOrigin(session.origin) + "\x00" + session.user.id : "anonymous"
}

export function loadWebExecution(session = loadGatewaySession()): WebExecution {
  try {
    const value = localStorage.getItem(directPrefix + scope(session)) ?? localStorage.getItem(legacyPrefix + scope(session))
    return value === "local" || value === "off" ? "off" : value === "cloud" ? "cloud" : "auto"
  } catch { return "auto" }
}

export function saveWebExecution(value: WebExecution, session = loadGatewaySession()) {
  if (!["auto", "off", "cloud"].includes(value)) throw new Error("invalid_web_execution")
  localStorage.setItem(directPrefix + scope(session), value)
  window.dispatchEvent(new Event(WEB_EXECUTION_EVENT))
}

export function loadPcWebExecution(session = loadGatewaySession()): PcWebExecution {
  try {
    // Read the original shared choice only as a migration source. New phone
    // choices never change a PC override, or vice versa.
    const value = localStorage.getItem(pcPrefix + scope(session)) ?? localStorage.getItem(legacyPrefix + scope(session))
    return value === "local" || value === "cloud" ? value : "inherit"
  } catch { return "inherit" }
}

export function savePcWebExecution(value: PcWebExecution, session = loadGatewaySession()) {
  if (!["inherit", "local", "cloud"].includes(value)) throw new Error("invalid_pc_web_execution")
  localStorage.setItem(pcPrefix + scope(session), value)
  window.dispatchEvent(new Event(WEB_EXECUTION_EVENT))
}

export function approvedWebSession(): GatewaySession | null {
  const session = loadGatewaySession()
  return session && session.user.enabled !== false && session.user.approval_status === "approved" && Date.parse(session.device.expires_at) > Date.now() ? session : null
}

export function sameWebAuthority(session: GatewaySession): boolean {
  const current = approvedWebSession()
  return Boolean(current && current.origin === session.origin && current.user.id === session.user.id && current.device.id === session.device.id && current.token === session.token)
}

/** An empty PC override follows that PC's configuration. Never send the phone's
 * account credential to a PC along with an execution preference. */
export function pcWebExecution(): "local" | "cloud" | "" {
  const selected = loadPcWebExecution()
  return selected === "inherit" ? "" : selected
}

export function watchWebAuthority(listener: () => void): () => void {
  window.addEventListener(GATEWAY_ACCOUNT_EVENT, listener)
  window.addEventListener(WEB_EXECUTION_EVENT, listener)
  window.addEventListener("storage", listener)
  return () => {
    window.removeEventListener(GATEWAY_ACCOUNT_EVENT, listener)
    window.removeEventListener(WEB_EXECUTION_EVENT, listener)
    window.removeEventListener("storage", listener)
  }
}
