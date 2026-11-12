import { useState } from "react"

import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import type { MessageKey } from "@/lib/i18n"
import { remoteAccountName } from "@/lib/remote-account-name"
import type { RemoteLoginStatus, RemoteStatus } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { ConfirmDeleteDialog } from "./confirm-delete-dialog"
import { Field, SettingsSection, settingsHintClass, settingsLabelClass } from "./settings-field"

export const loginStateMessage: Record<RemoteLoginStatus["state"], MessageKey> = {
  signed_out: "settings.remote.login.signedOut",
  signing_out: "desktopAccount.signingOut",
  starting: "settings.remote.login.starting",
  authorization_pending: "settings.remote.login.authorizationPending",
  account_pending: "settings.remote.login.accountPending",
  account_rejected: "settings.remote.login.accountRejected",
  authorized: "settings.remote.login.authorized",
  access_denied: "settings.remote.login.accessDenied",
  expired: "settings.remote.login.expired",
  error: "settings.remote.login.error",
  cancelled: "settings.remote.login.cancelled",
}
/** Stable host errors. Anything else stays a generic failure so a dial
 * detail cannot reach the page. */
const hostFailureMessage: Record<string, MessageKey> = {
  "remote: authorized host registration failed": "settings.remote.login.connectRetry",
  "remote: authorized host connection failed": "settings.remote.login.connectRetry",
  "remote needs hub_url": "settings.remote.login.hubMissing",
  "remote: gateway account login and approval are required": "settings.remote.login.approvalRequired",
  "remote: connection credential origin changed": "settings.remote.login.originChanged",
}

function accountLinkMessage(status: RemoteStatus): MessageKey | undefined {
  if ((status.login?.state ?? "signed_out") !== "authorized") return undefined
  if (!status.enabled) return "settings.remote.login.linkOff"
  if (status.online) return undefined
  if (status.error) return hostFailureMessage[status.error] ?? "settings.remote.login.connectFailed"
  return "settings.remote.login.connecting"
}

export const loginErrorMessage: Record<string, MessageKey> = {
  network_unavailable: "settings.remote.login.networkUnavailable",
  gateway_unavailable: "settings.remote.login.gatewayUnavailable",
  device_conflict: "settings.remote.login.deviceConflict",
  access_denied: "settings.remote.login.accessDenied",
  unsafe_response: "settings.remote.login.unsafeResponse",
  browser_unavailable: "settings.remote.login.browserUnavailable",
  credential_unavailable: "settings.remote.login.credentialUnavailable",
  logout_unsupported: "desktopAccount.logoutUnsupported",
  logout_denied: "desktopAccount.logoutDenied",
}

export function RemoteLoginSection({
  enabled, status, unavailable, busy, onStart, onCancel, onConnectionChange, accountEntry = false, query = "",
}: {
  enabled: boolean
  status?: RemoteStatus
  unavailable: boolean
  busy: boolean
  onStart: (newDevice: boolean) => void
  onCancel: () => void
  /** Saves this PC's connection. It does not sign out or start a new device login. */
  onConnectionChange?: (enabled: boolean) => void
  /** Explicit account entry may enable account mode in the same server-side action. */
  accountEntry?: boolean
  query?: string
}) {
  const t = useT()
  const [confirm, setConfirm] = useState(false)
  const login = status?.login
  const state = login?.state ?? "signed_out"
  const active = ["starting", "authorization_pending", "account_pending", "account_rejected"].includes(state)
  const ready = (enabled || accountEntry) && !!status?.login_available && !unavailable && !busy && state !== "signing_out"
  const message = !enabled && !accountEntry ? t("settings.remote.login.enableFirst")
    : unavailable ? t("settings.remote.login.statusUnavailable")
      : !status ? t("settings.remote.login.saving")
        : !status.login_available ? t("settings.remote.login.desktopRequired")
          : t(accountLinkMessage(status) ?? loginStateMessage[state] ?? "settings.remote.login.error")
  const details = unavailable ? undefined : login
  const accountName = remoteAccountName(details)
  const failure = details?.error_code ? loginErrorMessage[details.error_code] : undefined
  return (
    <SettingsSection title={t("settings.remote.login.title")} description={t("settings.remote.login.hint")}>
      {onConnectionChange ? (
        <Field
          query={query}
          label={t("settings.remote.enabled")}
          hint={t("settings.remote.enabledHint")}
        >
          <Switch checked={enabled} disabled={busy} onCheckedChange={onConnectionChange} />
        </Field>
      ) : null}
      <div className="flex flex-col gap-3 px-4 py-3" data-settings-row="">
        <div role="status" aria-live="polite" className={settingsLabelClass}>
          <p>{message}</p>
          {failure ? <p className={settingsHintClass}>{t(failure)}</p> : null}
        </div>
        {accountName ? <p className={settingsHintClass}>{t("settings.remote.login.account", { name: accountName })}</p> : null}
        {details?.user_code ? (
          <div>
            <p className={settingsHintClass}>{t("settings.remote.login.code")}</p>
            <code className="select-all font-mono text-sm">{details.user_code}</code>
          </div>
        ) : null}
        {details?.host_fp ? (
          <div>
            <p className={settingsHintClass}>{t("settings.remote.login.fingerprint")}</p>
            <code className="select-all break-all font-mono text-sm">{details.host_fp}</code>
          </div>
        ) : null}
        <div className="flex flex-wrap gap-2">
          {active ? (
            <Button type="button" variant="outline" size="sm" disabled={!ready} onClick={onCancel}>
              {t("settings.remote.login.cancel")}
            </Button>
          ) : !accountEntry || state !== "authorized" ? (
            <Button type="button" size="sm" disabled={!ready} onClick={() => onStart(false)}>
              {t("settings.remote.login.start")}
            </Button>
          ) : null}
          {(status?.auth_mode === "account" && status.has_token) || state === "error" || state === "access_denied" ? (
            <Button type="button" variant="outline" size="sm" disabled={!ready || active} onClick={() => setConfirm(true)}>
              {t("settings.remote.login.reconnect")}
            </Button>
          ) : null}
        </div>
      </div>
      <ConfirmDeleteDialog
        open={confirm}
        title={t("settings.remote.login.reconnectTitle")}
        description={t("settings.remote.login.reconnectHint")}
        confirmLabel={t("settings.remote.login.reconnectConfirm")}
        cancelLabel={t("project.cancel")}
        onOpenChange={setConfirm}
        onConfirm={() => { if (ready) onStart(true) }}
      />
    </SettingsSection>
  )
}
