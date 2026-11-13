import type { AccountModels, RemoteLoginStatus } from "./types"

// Registrable domains an account picture may come from. Subdomains belong to
// that site; every other host stays unrequested.
const avatarTrustedRoots = ["fofa.info", "nosec.org", "baimaohui.net"]

// A public image address on one of those roots. The same rule as the private
// desktop receipt: absolute http(s), no userinfo, fragment, whitespace,
// control character or backslash, and at most 2048 bytes. The query string
// stays.
export function acceptAvatarURL(raw: unknown): string | undefined {
  if (typeof raw !== "string") return undefined
  const value = raw.trim()
  if (!value.startsWith("https://") && !value.startsWith("http://")) return undefined
  if (new TextEncoder().encode(value).length > 2048) return undefined
  if (/[ \t\r\n\\#\u0000-\u001F\u007F]/.test(value) || [...value].some((char) => /\s/u.test(char))) return undefined
  let url: URL
  try { url = new URL(value) } catch { return undefined }
  if (url.username || url.password || url.hash || !url.hostname || (url.protocol !== "https:" && url.protocol !== "http:") || !avatarHostTrusted(url.hostname)) return undefined
  return value
}

function avatarHostTrusted(hostname: string): boolean {
  const host = hostname.toLowerCase().replace(/\.$/, "")
  const labels = host.split(".")
  if (labels.length < 2 || labels.some((label) => label === "")) return false
  return avatarTrustedRoots.some((root) => host === root || host.endsWith(`.${root}`))
}

export function accountAvatarURL(login?: RemoteLoginStatus, models?: AccountModels): string | undefined {
  if (login && (login.state === "authorized" || login.state === "signing_out")) {
    const fromLogin = acceptAvatarURL(login.avatar_url)
    if (fromLogin) return fromLogin
  }
  if (models && models.state !== "signed_out") return acceptAvatarURL(models.avatar_url)
  return undefined
}
