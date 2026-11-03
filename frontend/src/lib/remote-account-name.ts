import type { RemoteLoginStatus } from "./types"

// Previous Gateway receipts only contain the generated account identifier.
const generatedNOSECUsername = /^nosec_([\p{L}\p{Nd}_-]{0,24})_[0-9a-f]{16}$/u

export function remoteAccountName(login?: RemoteLoginStatus): string {
  const displayName = login?.display_name?.trim()
  if (displayName) return displayName
  const username = login?.username?.trim() ?? ""
  return generatedNOSECUsername.exec(username)?.[1] ?? username
}
