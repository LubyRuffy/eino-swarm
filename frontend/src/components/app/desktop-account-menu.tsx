import { Loader2, UserRound } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel,
  DropdownMenuSeparator, DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { api } from "@/lib/api"
import { errorMessage, toastError } from "@/store/toasts"
import { refreshMetaAndModels } from "@/store/app-models"
import { accountAvatarURL } from "@/lib/avatar-url"
import type { MessageKey } from "@/lib/i18n"
import { remoteAccountName } from "@/lib/remote-account-name"
import { defaultRemoteSettings, type RemoteLoginStatus, type RemoteStatus } from "@/lib/types"
import { useDesktopAccount } from "@/lib/use-remote-status"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { openSettings } from "@/store/settings-sheet"
import { RemoteLoginSection } from "./settings-remote-login"

const summaries: Record<RemoteLoginStatus["state"], MessageKey> = {
  signed_out: "desktopAccount.local",
  signing_out: "desktopAccount.signingOut",
  starting: "desktopAccount.starting",
  authorization_pending: "desktopAccount.authorizationPending",
  account_pending: "desktopAccount.accountPending",
  account_rejected: "desktopAccount.accountRejected",
  authorized: "desktopAccount.online",
  access_denied: "desktopAccount.accessDenied",
  expired: "desktopAccount.expired",
  error: "desktopAccount.error",
  cancelled: "desktopAccount.cancelled",
}

function summaryKey(status: RemoteStatus | undefined, unavailable: boolean): MessageKey {
  if (unavailable) return "desktopAccount.unavailable"
  if (!status) return "desktopAccount.loading"
  if (status.auth_mode !== "account") return "desktopAccount.local"
  const state = status.login?.state ?? "signed_out"
  if (state === "authorized") {
    // A disabled link never dials. That is not the same as an enabled PC
    // that is still offline, and a recorded failure is not still "waiting".
    if (!status.enabled) return "desktopAccount.linkOff"
    if (status.online) return "desktopAccount.online"
    if (status.error) return "desktopAccount.connectFailed"
    return "desktopAccount.offline"
  }
  return summaries[state] ?? "desktopAccount.error"
}

/** A local account entry, independent of the transcript and the Phone settings
 * draft. Go owns browser authorization; this component sees safe status only. */
export function DesktopAccountMenu() {
  const t = useT()
  const connection = useDesktopAccount()
  const { status, unavailable, busy } = connection
  const [open, setOpen] = useState(false)
  const [dialog, setDialog] = useState(false)
  const [failed, setFailed] = useState(false)
  const trigger = useRef<HTMLButtonElement>(null)
  const handoff = useRef(false)
  const managed = status?.auth_mode === "account"
  const login = managed ? status.login : undefined
  const models = useApp((s) => s.meta?.account_models)
  const fofa = useApp((s) => s.meta?.product_mode) === "fofa"
  const clicks = useRef(0)
  const clickTimer = useRef<number | undefined>(undefined)
  const unlocking = useRef(false)
  useEffect(() => () => window.clearTimeout(clickTimer.current), [])
  const [pictureFailed, setPictureFailed] = useState(false)
  const authorized = !unavailable && login?.state === "authorized"
  const signingOut = login?.state === "signing_out"
  const accountName = remoteAccountName(unavailable ? undefined : login)
  const initial = (authorized || signingOut) && accountName ? Array.from(accountName)[0]?.toLocaleUpperCase(t.locale) : undefined
  const picture = accountAvatarURL(unavailable ? undefined : login, models)
  useEffect(() => { setPictureFailed(false) }, [picture])
  const pending = login && !["signed_out", "cancelled"].includes(login.state)
  const canLogout = managed && !unavailable && (status.has_token || !!pending)
  const summary = t(summaryKey(status, unavailable))
  const ready = !!status?.login_available && !busy && !unavailable && !signingOut

  const showDialog = () => {
    handoff.current = true
    setFailed(false)
    setOpen(false)
    setDialog(true)
  }
  const run = (action: () => Promise<RemoteStatus>) => {
    setFailed(false)
    void connection.perform(action).catch(() => setFailed(true))
  }
  const start = (newDevice: boolean) => run(() => api.startRemoteLogin(newDevice, true))
  const setConnection = (enabled: boolean) => run(async () => {
    const current = await api.settings()
    await api.saveSettings({ remote: { ...defaultRemoteSettings(current.remote), enabled } })
    return api.remoteStatus()
  })
  const logout = () => run(api.logoutRemote)
  const onPortraitClick = () => {
    clicks.current += 1
    window.clearTimeout(clickTimer.current)
    clickTimer.current = window.setTimeout(() => { clicks.current = 0 }, 1200)
    if (clicks.current < 3) return
    clicks.current = 0
    window.clearTimeout(clickTimer.current)
    if (unlocking.current) return
    const mode = useApp.getState().meta?.product_mode === "local" ? "fofa" : "local"
    unlocking.current = true
    void api.saveSettings({ product: { mode } })
      .then(() => refreshMetaAndModels())
      .catch((error) => toastError(errorMessage(error)))
      .finally(() => { unlocking.current = false })
  }
  const settings = () => {
    handoff.current = true
    setOpen(false)
    setDialog(false)
    openSettings("remote")
  }

  return (
    <div className="shrink-0">
      <DropdownMenu open={open} onOpenChange={setOpen} modal={false}>
        <DropdownMenuTrigger asChild>
          <Button
            ref={trigger}
            type="button"
            variant="ghost"
            size="icon-sm"
            data-testid="desktop-account-trigger"
            aria-label={t("desktopAccount.title")}
            title={`${t("desktopAccount.title")}: ${summary}`}
            className="rounded-full text-sidebar-foreground hover:bg-sidebar-accent"
            style={{ width: "var(--sidebar-row-height)", height: "var(--sidebar-row-height)" }}
          >
            <span aria-hidden="true" className="flex size-full items-center justify-center overflow-hidden rounded-full">
              {picture && !pictureFailed ? (
                <img src={picture} alt="" referrerPolicy="no-referrer" className="size-full object-cover" onError={() => setPictureFailed(true)} />
              ) : initial ? (
                <span className="flex size-full items-center justify-center bg-sidebar-accent text-xs font-medium">{initial}</span>
              ) : busy || (!unavailable && login?.state === "starting") ? (
                <Loader2 className="animate-spin" />
              ) : <UserRound />}
            </span>
          </Button>
        </DropdownMenuTrigger>
        {/* Unmount a closed menu before it can refocus during an exit animation.
            Handoffs leave focus to the account dialog or Settings sheet. */}
        {open ? <DropdownMenuContent
          side="top"
          align="start"
          className="max-w-72"
          onCloseAutoFocus={(event) => {
            if (handoff.current) { event.preventDefault(); handoff.current = false }
          }}
        >
          <DropdownMenuLabel>
            {accountName ? <p className="truncate text-foreground">{accountName}</p> : null}
            <p role="status" aria-live="polite" className="font-normal">{summary}</p>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          {login && login.state !== "signed_out" && login.state !== "cancelled" ? (
            <DropdownMenuItem onSelect={showDialog}>{t("desktopAccount.details")}</DropdownMenuItem>
          ) : (
            <DropdownMenuItem disabled={!ready} onSelect={() => { showDialog(); start(false) }}>
              {t("settings.remote.login.start")}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem disabled={busy} onSelect={() => { void connection.refresh() }}>
            {t("desktopAccount.refresh")}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={settings}>{t("desktopAccount.settings")}</DropdownMenuItem>
          {canLogout ? (
            <DropdownMenuItem disabled={busy || signingOut} onSelect={() => { showDialog(); logout() }}>
              {t("desktopAccount.signOut")}
            </DropdownMenuItem>
          ) : null}
        </DropdownMenuContent> : null}
      </DropdownMenu>
      {dialog ? (
        <Dialog open modal={false} onOpenChange={setDialog}>
          <DialogContent
            data-testid="desktop-account-dialog"
            className="max-h-[90dvh] overflow-y-auto"
            onCloseAutoFocus={(event) => {
              event.preventDefault()
              if (handoff.current) { handoff.current = false; return }
              trigger.current?.focus()
            }}
          >
            <DialogHeader>
              <DialogTitle>{t("desktopAccount.title")}</DialogTitle>
              <DialogDescription>{t("desktopAccount.description")}</DialogDescription>
            </DialogHeader>
            <div className="flex justify-center">
              <button
                type="button"
                data-testid="account-portrait"
                aria-label={accountName || undefined}
                tabIndex={-1}
                className="flex size-16 items-center justify-center overflow-hidden rounded-full bg-muted text-foreground"
                onClick={onPortraitClick}
              >
                {picture && !pictureFailed ? (
                  <img src={picture} alt="" referrerPolicy="no-referrer" className="size-full object-cover" onError={() => setPictureFailed(true)} />
                ) : initial ? (
                  <span className="text-xl font-medium">{initial}</span>
                ) : <UserRound className="size-7" />}
              </button>
            </div>
            {fofa ? null : <p className="text-sm text-muted-foreground">{t("desktopAccount.localHint")}</p>}
            {failed ? <p role="alert" className="text-sm text-destructive">{t("desktopAccount.actionFailed")}</p> : null}
            <RemoteLoginSection
              accountEntry
              enabled={!!status?.enabled}
              status={status}
              unavailable={unavailable}
              busy={busy}
              onStart={start}
              onCancel={() => run(api.cancelRemoteLogin)}
              onConnectionChange={setConnection}
            />
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => { void connection.refresh() }}>
                {t("desktopAccount.refresh")}
              </Button>
              <Button type="button" variant="ghost" size="sm" onClick={settings}>
                {t("desktopAccount.settings")}
              </Button>
              {canLogout ? (
                <Button type="button" variant="outline" size="sm" disabled={busy || signingOut} onClick={logout}>
                  {t("desktopAccount.signOut")}
                </Button>
              ) : null}
            </div>
          </DialogContent>
        </Dialog>
      ) : null}
    </div>
  )
}
