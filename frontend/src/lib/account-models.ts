import type { MessageKey } from "./i18n"
import type { AccountModels, Meta, ModelInfo, ModelSource, RemoteLoginStatus } from "./types"

const reasons: Record<string, MessageKey> = {
  rejected: "accountModels.reason.rejected",
  unsupported: "accountModels.reason.unsupported",
  network: "accountModels.reason.network",
  gateway: "accountModels.reason.gateway",
  unsafe_response: "accountModels.reason.unsafe_response",
  no_models: "accountModels.reason.no_models",
}

/** Why the server's models are missing, or undefined when nothing needs
 *  explaining (not signed in, still reading, or ready). */
export function accountModelsProblem(account?: AccountModels): MessageKey | undefined {
  if (!account) return undefined
  if (account.state === "no_access") return "accountModels.noAccess"
  if (account.state !== "unavailable") return undefined
  return reasons[account.reason ?? ""] ?? "accountModels.reason.unknown"
}

/** First-run welcome: nothing can run yet and the person has not answered. */
export function showWelcome(meta?: Meta): boolean {
  if (!meta || meta.startup === "restoring") return false
  return !meta.configured && !meta.ui?.welcome_dismissed
}

/** The models the server account supplies, in the order the server sent. */
export function accountModelRows(models: ModelInfo[]): ModelInfo[] {
  return models.filter((m) => m.source === "account" && m.ready && m.model)
}

/** New conversations start on the server only when that source is selected
 *  and its models are actually available. An unset source is the server.
 *  Otherwise they start on the configured provider, including while the
 *  account catalog is still missing. */
export function serverStartsConversations(
  source: ModelSource | undefined,
  accountReady: boolean,
): boolean {
  return accountReady && source !== "provider"
}

/** The local account state moves before the model list does: Go refreshes the
 *  list after sign-in and sign-out finish. True while the two disagree, which
 *  is when the UI should look again instead of showing a stale list. */
export function accountModelsStale(
  login: RemoteLoginStatus["state"] | undefined,
  account: AccountModels | undefined,
): boolean {
  if (!login || !account) return false
  if (login === "authorized") return account.state === "signed_out" || account.state === "pending"
  if (login === "signed_out" || login === "cancelled") {
    return account.state !== "signed_out"
  }
  return false
}
