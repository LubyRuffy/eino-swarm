import { SelectItem } from "@/components/ui/select"
import { accountKindTally, accountModelsProblem, officialModelLock, type AccountKindKey } from "@/lib/account-models"
import type { MessageKey } from "@/lib/i18n"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

import { AccountSignIn } from "./account-sign-in"
import {
  SettingsChoice, SettingsRow, SettingsSection, settingsHintClass, settingsMatch,
} from "./settings-field"

/** The signed-in server account, ahead of the person's own model services.
 *  The line is a count by the server's classification. The model names and
 *  the conversation default live on the server row below. */
export function AccountModelsCard({
  settings, onChange, query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const account = useApp((s) => s.meta?.account_models)
  const problem = accountModelsProblem(account)
  const ready = account?.state === "ready"
  const tally = accountKindTally(account?.kinds)
  const ownReady = settings.models.providers.some((p) => p.ready && p.enabled !== false)
  const source = settings.models.default_source ?? "account"
  const officialOnly = officialModelLock(account)
  const presentation = settings.models.account_presentation === "full" ? "full" : "official"
  const kindText: Record<AccountKindKey, MessageKey> = {
    chat: "accountModels.kind.chat",
    unclassified: "accountModels.kind.unclassified",
    embedding: "accountModels.kind.embedding",
    ocr: "accountModels.kind.ocr",
    decisions: "accountModels.kind.decisions",
    other: "accountModels.kind.other",
  }
  const counted = tally.map((part) => t(kindText[part.key], { count: part.count })).join(" · ")
  const summary = !account || account.state === "signed_out"
    ? t("accountModels.signedOut")
    : account.state === "pending"
      ? t("accountModels.pending")
      : ready
        ? counted || (account.username
          ? t("accountModels.readyAs", { count: account.models, name: account.display_name || account.username })
          : t("accountModels.ready", { count: account.models }))
        : t(problem ?? "accountModels.reason.unknown")

  if (!settingsMatch(query, t("accountModels.title"), t("accountModels.desc"), "account server official")) return null
  return (
    <SettingsSection title={t("accountModels.title")} description={t("accountModels.desc")}>
      <div className="flex flex-col gap-3 px-4 py-3" data-settings-row="" data-testid="account-models-card">
        <div role="status" aria-live="polite">
          <p className={settingsHintClass}>{summary}</p>
        </div>
        {ready ? null : <AccountSignIn />}
      </div>
      {account?.profile ? (
        <SettingsChoice
          label={t("accountModels.presentationTitle")}
          value={presentation}
          ariaLabel={t("accountModels.presentationTitle")}
          onValueChange={(next) =>
            onChange({
              ...settings,
              models: {
                ...settings.models,
                account_presentation: next === "full" ? "full" : "official",
              },
            })}
        >
          <SelectItem value="official">{t("accountModels.presentationOfficial")}</SelectItem>
          <SelectItem value="full">{t("accountModels.presentationFull")}</SelectItem>
        </SettingsChoice>
      ) : null}
      {ready && ownReady && !officialOnly ? (
        <SettingsChoice
          label={t("accountModels.defaultTitle")}
          value={source}
          ariaLabel={t("accountModels.defaultTitle")}
          onValueChange={(next) =>
            onChange({
              ...settings,
              models: { ...settings.models, default_source: next === "provider" ? "provider" : "account" },
            })}
        >
          <SelectItem value="account">{t("accountModels.defaultAccount")}</SelectItem>
          <SelectItem value="provider">{t("accountModels.defaultProvider")}</SelectItem>
        </SettingsChoice>
      ) : null}
      {ready && !ownReady && !officialOnly ? (
        <SettingsRow label={t("accountModels.defaultTitle")} hint={t("accountModels.defaultNeedsOwn")} />
      ) : null}
    </SettingsSection>
  )
}
