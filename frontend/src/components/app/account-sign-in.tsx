import { useState } from "react"

import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import type { RemoteStatus } from "@/lib/types"
import { useDesktopAccount } from "@/lib/use-remote-status"
import { useT } from "@/lib/use-t"

import { loginErrorMessage, loginStateMessage } from "./settings-remote-login"
import { settingsHintClass, settingsLabelClass } from "./settings-field"

const inFlight = ["starting", "authorization_pending", "account_pending", "account_rejected"]

/** The one-button path to the server account: start, show the browser
 *  confirmation code, cancel. Go owns the browser step; this only reads safe
 *  status. Signing in enables the account link in the same server action. */
export function AccountSignIn({ compact = false }: { compact?: boolean }) {
  const t = useT()
  const connection = useDesktopAccount()
  const { status, unavailable, busy } = connection
  const [failed, setFailed] = useState(false)
  const login = unavailable ? undefined : status?.login
  const state = login?.state ?? "signed_out"
  const active = inFlight.includes(state)
  const canAct = !!status?.login_available && !unavailable && !busy && state !== "signing_out"
  const failure = login?.error_code ? loginErrorMessage[login.error_code] : undefined
  const run = (action: () => Promise<RemoteStatus>) => {
    setFailed(false)
    void connection.perform(action).catch(() => setFailed(true))
  }
  // A signed-out account has nothing to report; every other state does.
  const showState = state !== "signed_out" && state !== "authorized"
  // A browser build cannot open the system browser, so a banner has no
  // sign-in to offer; the models page explains why.
  if (compact && status && !status.login_available) return null

  return (
    <div
      className={compact ? "flex flex-wrap items-center gap-2" : "flex flex-col gap-3"}
      data-testid="account-sign-in"
    >
      <div role="status" aria-live="polite" className="flex flex-col gap-1">
        {showState ? <p className={settingsLabelClass}>{t(loginStateMessage[state])}</p> : null}
        {failure ? <p className={settingsHintClass}>{t(failure)}</p> : null}
        {unavailable ? <p className={settingsHintClass}>{t("settings.remote.login.statusUnavailable")}</p> : null}
        {status && !status.login_available
          ? <p className={settingsHintClass}>{t("settings.remote.login.desktopRequired")}</p>
          : null}
      </div>
      {failed ? <p role="alert" className="text-sm text-destructive">{t("desktopAccount.actionFailed")}</p> : null}
      {login?.user_code ? (
        <div className={compact ? "flex items-center gap-2" : undefined}>
          <p className={settingsHintClass}>{t("settings.remote.login.code")}</p>
          <code className="select-all font-mono text-sm">{login.user_code}</code>
        </div>
      ) : null}
      {state === "authorized" ? null : (
        <div className="flex flex-wrap gap-2">
          {active ? (
            <Button type="button" variant="outline" size="sm" disabled={!canAct} onClick={() => run(api.cancelRemoteLogin)}>
              {t("settings.remote.login.cancel")}
            </Button>
          ) : (
            <Button type="button" size="sm" disabled={!canAct} onClick={() => run(() => api.startRemoteLogin(false, true))}>
              {t("accountModels.signIn")}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
