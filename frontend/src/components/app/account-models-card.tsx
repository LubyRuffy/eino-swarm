import { Badge } from "@/components/ui/badge"
import { SelectItem } from "@/components/ui/select"
import { accountModelRows, accountModelsProblem } from "@/lib/account-models"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

import { AccountSignIn } from "./account-sign-in"
import {
  SettingsChoice, SettingsRow, SettingsSection, settingsHintClass, settingsMatch,
} from "./settings-field"

const SHOWN = 12

/** The signed-in server account's models, ahead of the person's own model
 *  services. Models are read-only here: the server decides them. What the
 *  person chooses is which source a new conversation starts on. */
export function AccountModelsCard({
  settings, onChange, query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const account = useApp((s) => s.meta?.account_models)
  const models = useApp((s) => s.models)
  const rows = accountModelRows(models)
  const problem = accountModelsProblem(account)
  const ready = account?.state === "ready"
  const ownReady = settings.models.providers.some((p) => p.ready && p.enabled !== false)
  const source = settings.models.default_source ?? "account"
  const summary = !account || account.state === "signed_out"
    ? t("accountModels.signedOut")
    : account.state === "pending"
      ? t("accountModels.pending")
      : ready
        ? account.username
          ? t("accountModels.readyAs", { count: account.models, name: account.display_name || account.username })
          : t("accountModels.ready", { count: account.models })
        : t(problem ?? "accountModels.reason.unknown")

  if (!settingsMatch(query, t("accountModels.title"), t("accountModels.desc"), "account server official")) return null
  return (
    <SettingsSection title={t("accountModels.title")} description={t("accountModels.desc")}>
      <div className="flex flex-col gap-3 px-4 py-3" data-settings-row="" data-testid="account-models-card">
        <div role="status" aria-live="polite">
          <p className={settingsHintClass}>{summary}</p>
        </div>
        {ready && rows.length ? (
          <ul aria-label={t("accountModels.listLabel")} className="flex flex-wrap gap-1.5">
            {rows.slice(0, SHOWN).map((m) => (
              <li key={m.id}><Badge variant="outline">{m.label || m.model}</Badge></li>
            ))}
            {rows.length > SHOWN ? <li><Badge variant="outline">+{rows.length - SHOWN}</Badge></li> : null}
          </ul>
        ) : null}
        {ready ? null : <AccountSignIn />}
      </div>
      {ready && ownReady ? (
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
      {ready && !ownReady ? (
        <SettingsRow label={t("accountModels.defaultTitle")} hint={t("accountModels.defaultNeedsOwn")} />
      ) : null}
    </SettingsSection>
  )
}
