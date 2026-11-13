// A public image address only. Absolute http(s), no userinfo, fragment,
// whitespace, control character or backslash, and at most 2048 bytes.
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
