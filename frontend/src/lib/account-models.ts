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

/** First-run welcome: nothing can run yet and the person has not answered.
 *  A FOFA install uses the sign-in gate instead of offering another model. */
export function showWelcome(meta?: Meta): boolean {
  if (!meta || meta.startup === "restoring" || meta.product_mode === "fofa") return false
  return !meta.configured && !meta.ui?.welcome_dismissed
}

/** FOFA with nobody signed in. The account button stays reachable behind it. */
export function showLoginGate(meta?: Meta): boolean {
  if (!meta || meta.startup === "restoring" || meta.product_mode !== "fofa") return false
  const state = meta.account_models?.state
  return !state || state === "signed_out"
}

/** The models the server account supplies, in the order the server sent.
 *  The composer list is already limited to conversation models. */
export function accountModelRows(models: ModelInfo[]): ModelInfo[] {
  return models.filter((m) => m.source === "account" && m.ready && m.model)
}

const kindOrder = ["chat", "unclassified", "embedding", "ocr", "decisions", "other"] as const
export type AccountKindKey = (typeof kindOrder)[number]

/** Non-zero classification counts, in a stable order. Missing kinds means the
 *  server has not reported them yet. */
export function accountKindTally(kinds?: AccountModels["kinds"]): Array<{ key: AccountKindKey; count: number }> {
  if (!kinds) return []
  return kindOrder.flatMap((key) => {
    const count = kinds[key] ?? 0
    return count > 0 ? [{ key, count }] : []
  })
}

/** Official presentation is the one-name lock. Full, or no profile, keeps the list. */
export function officialModelLock(account?: AccountModels): boolean {
  return Boolean(account?.profile && account.profile.presentation !== "full")
}

/** The conversation model Settings shows for the server. A saved name stays
 *  even after it leaves the list. Empty follows the listed default.
 *  A profile label replaces a saved name the list no longer contains, so a
 *  previous vendor id is not shown. */
export function accountChoiceLabel(models: ModelInfo[], chosen?: string): string {
  const rows = accountModelRows(models)
  const want = chosen?.trim() ?? ""
  const match = want ? rows.find((row) => row.model === want) : undefined
  const row = match ?? (want ? undefined : rows.find((item) => item.default) ?? rows[0])
  if (row?.label && row.label !== row.model) return row.label
  const branded = rows[0]
  if (want && !match && branded?.label && branded.label !== branded.model) return branded.label
  if (want) return want
  return row?.model ?? ""
}

/** The conversation model Settings shows for the server. A saved name stays
 *  even after it leaves the list. Empty follows the listed default. */
export function effectiveAccountModel(models: ModelInfo[], chosen?: string): string {
  const rows = accountModelRows(models)
  const want = chosen?.trim()
  if (want) return want
  return rows.find((row) => row.default)?.model || rows[0]?.model || ""
}

/** Embedding models the signed-in server classified. Other catalogs stay out. */
export function embeddingModelChoices(account?: AccountModels): Array<{
  providerId: string
  model: string
  label: string
  providerLabel: string
}> {
  const providerId = account?.source_id?.trim() ?? ""
  if (!providerId) return []
  const providerLabel = account?.label?.trim() || providerId
  const seen = new Set<string>()
  const out: Array<{ providerId: string; model: string; label: string; providerLabel: string }> = []
  for (const raw of account?.kinds?.embedding_models ?? []) {
    const model = raw.trim()
    if (!model || seen.has(model)) continue
    seen.add(model)
    out.push({ providerId, model, label: model, providerLabel })
  }
  return out
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
