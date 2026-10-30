import { useState } from "react"

import { Button } from "@/components/ui/button"
import type { MessageKey } from "@/lib/i18n"
import type { RemoteLoginStatus, RemoteStatus } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { ConfirmDeleteDialog } from "./confirm-delete-dialog"
import { SettingsSection, settingsHintClass, settingsLabelClass } from "./settings-field"

const stateMessage: Record<RemoteLoginStatus["state"], MessageKey> = {
  signed_out: "settings.remote.login.signedOut",
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
const errorMessage: Record<string, MessageKey> = {
  network_unavailable: "settings.remote.login.networkUnavailable",
  gateway_unavailable: "settings.remote.login.gatewayUnavailable",
  device_conflict: "settings.remote.login.deviceConflict",
  access_denied: "settings.remote.login.accessDenied",
  unsafe_response: "settings.remote.login.unsafeResponse",
  browser_unavailable: "settings.remote.login.browserUnavailable",
  credential_unavailable: "settings.remote.login.credentialUnavailable",
}

export function RemoteLoginSection({
  enabled, status, unavailable, busy, onStart, onCancel,
}: {
  enabled: boolean
  status?: RemoteStatus
  unavailable: boolean
  busy: boolean
  onStart: (newDevice: boolean) => void
  onCancel: () => void
}) {
  const t = useT()
  const [confirm, setConfirm] = useState(false)
  const login = status?.login
  const state = login?.state ?? "signed_out"
  const active = ["starting", "authorization_pending", "account_pending", "account_rejected"].includes(state)
  const ready = enabled && !!status?.login_available && !unavailable && !busy
  const message = !enabled ? t("settings.remote.login.enableFirst")
    : unavailable ? t("settings.remote.login.statusUnavailable")
      : !status ? t("settings.remote.login.saving")
        : !status.login_available ? t("settings.remote.login.desktopRequired")
          : state === "authorized" && !status.online ? t("settings.remote.login.connecting")
            : t(stateMessage[state] ?? "settings.remote.login.error")
  const failure = login?.error_code ? errorMessage[login.error_code] : undefined
  return (
    <SettingsSection title={t("settings.remote.login.title")} description={t("settings.remote.login.hint")}>
      <div className="flex flex-col gap-3 px-4 py-3" data-settings-row="">
        <div role="status" aria-live="polite" className={settingsLabelClass}>
          <p>{message}</p>
          {failure ? <p className={settingsHintClass}>{t(failure)}</p> : null}
        </div>
        {login?.username ? <p className={settingsHintClass}>{t("settings.remote.login.account", { name: login.username })}</p> : null}
        {login?.user_code ? (
          <div>
            <p className={settingsHintClass}>{t("settings.remote.login.code")}</p>
            <code className="select-all font-mono text-sm">{login.user_code}</code>
          </div>
        ) : null}
        {login?.host_fp ? (
          <div>
            <p className={settingsHintClass}>{t("settings.remote.login.fingerprint")}</p>
            <code className="select-all break-all font-mono text-sm">{login.host_fp}</code>
          </div>
        ) : null}
        <div className="flex flex-wrap gap-2">
          {active ? (
            <Button type="button" variant="outline" size="sm" disabled={!ready} onClick={onCancel}>
              {t("settings.remote.login.cancel")}
            </Button>
          ) : (
            <Button type="button" size="sm" disabled={!ready} onClick={() => onStart(false)}>
              {t("settings.remote.login.start")}
            </Button>
          )}
          {status?.has_token || state === "error" || state === "access_denied" ? (
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
