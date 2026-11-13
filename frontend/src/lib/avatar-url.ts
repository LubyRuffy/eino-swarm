import type { AccountModels, RemoteLoginStatus } from "./types"

// A public image address only. The same rule as the private desktop receipt:
// absolute http(s), no userinfo, fragment, whitespace, control character or
// backslash, and at most 2048 bytes. The query string stays.
export function acceptAvatarURL(raw: unknown): string | undefined {
  if (typeof raw !== "string") return undefined
  const value = raw.trim()
  if (!value.startsWith("https://") && !value.startsWith("http://")) return undefined
  if (new TextEncoder().encode(value).length > 2048) return undefined
  if (/[ \t\r\n\\#\u0000-\u001F\u007F]/.test(value) || [...value].some((char) => /\s/u.test(char))) return undefined
  let url: URL
  try { url = new URL(value) } catch { return undefined }
  if (url.username || url.password || url.hash || !url.hostname || (url.protocol !== "https:" && url.protocol !== "http:")) return undefined
  return value
}

export function accountAvatarURL(login?: RemoteLoginStatus, models?: AccountModels): string | undefined {
  if (login && (login.state === "authorized" || login.state === "signing_out")) {
    const fromLogin = acceptAvatarURL(login.avatar_url)
    if (fromLogin) return fromLogin
  }
  if (models && models.state !== "signed_out") return acceptAvatarURL(models.avatar_url)
  return undefined
}
