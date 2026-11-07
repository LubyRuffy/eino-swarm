import type { RemoteLoginStatus } from "./types"

// Gateway receipts may only contain a generated account identifier.
const generatedGatewayUsername = /^nosec_([\p{L}\p{Nd}_-]{0,24})_[0-9a-f]{16}$/u

export function remoteAccountName(login?: RemoteLoginStatus): string {
  const displayName = login?.display_name?.trim()
  if (displayName) return displayName
  const username = login?.username?.trim() ?? ""
  return generatedGatewayUsername.exec(username)?.[1] ?? username
}
