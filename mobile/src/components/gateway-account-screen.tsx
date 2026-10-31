import { useState } from "react"
import { Loader2, Monitor, X } from "lucide-react"
import type { GatewayAccountController } from "@/components/gateway-account-shell"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import type { DirectProvider } from "@/lib/direct-provider"
import { t } from "@/lib/i18n"
import { GatewayError } from "@/lib/gateway-account"

export function GatewayAccountScreen({ account, providers, modelBusy, modelError, onClose, onConnect, onRefresh, onOpenChat }: {
  account: GatewayAccountController
  providers: DirectProvider[]
  modelBusy: boolean
  modelError: boolean
  onClose?: () => void
  onOpenChat?: () => void
  onConnect: (id: string) => Promise<void>
  onRefresh: () => Promise<void>
}) {
  const [connecting, setConnecting] = useState("")
  const [connectionError, setConnectionError] = useState("")
  const [advanced, setAdvanced] = useState(false)
  const [server, setServer] = useState(account.origin)
  const authorized = account.status === "authorized"
  const waiting = ["checking", "authorization_pending", "account_pending", "account_rejected"].includes(account.status) || Boolean(account.session && account.status === "access_denied")
  const statusText = account.status === "account_pending" ? t("account.pending")
    : account.status === "account_rejected" ? t("account.rejected")
    : account.status === "authorization_pending" ? t("account.browser")
    : account.status === "checking" ? t("account.checking")
    : account.status === "access_denied" ? t(account.session ? "account.accessDenied" : "account.denied")
    : account.status === "expired" ? t("account.expired") : ""
  const connect = async (id: string) => {
    setConnecting(id); setConnectionError("")
    try { await onConnect(id) } catch (error) { setConnectionError(t(error instanceof GatewayError && [401, 403].includes(error.status) ? "account.expired" : error instanceof GatewayError && error.status === 409 ? "scan.hostOffline" : "account.connectionFailed")) }
    finally { setConnecting("") }
  }
  return (
    <main data-testid="gateway-account" className="mx-auto flex h-full max-w-lg flex-col overflow-y-auto pb-[env(safe-area-inset-bottom)]">
      <header className="flex h-12 shrink-0 items-center border-b border-border px-4">
        <h1 className="flex-1 text-base font-semibold">{t("account.title")}</h1>
        {onClose ? <Button variant="ghost" className="size-10 px-0" aria-label={t("home.close")} onClick={onClose}><X className="size-5" aria-hidden /></Button> : null}
      </header>
      <div className="flex flex-col gap-4 p-5">
        {account.session ? <p className="break-all text-sm font-medium">{account.session.user.username}</p> : null}
        {statusText ? <p role="status" className="text-sm text-muted-foreground">{statusText}</p> : null}
        {account.error ? <p role="alert" className="text-sm text-destructive">{t("chat.failed")}</p> : null}
        {!authorized && (!waiting || account.status === "access_denied") ? <>
          <p className="text-sm leading-relaxed text-muted-foreground">{t("account.hint")}</p>
          <Button disabled={account.busy} onClick={() => void account.login()}>{t("account.login")}</Button>
          {onClose ? <Button variant="ghost" onClick={onClose}>{t("account.anonymous")}</Button> : null}
        </> : null}
        {authorized ? <>
          <section className="flex flex-col gap-2">
            <h2 className="text-sm font-semibold">{t("account.models")}</h2>
            <p className="text-xs text-muted-foreground">{t("account.modelsHint")}</p>
            {onOpenChat ? <Button variant="outline" onClick={onOpenChat}>{t("account.history")}</Button> : null}
            {!account.bootstrap?.models.api_access_enabled ? <p className="text-sm text-muted-foreground">{t("account.modelsDenied")}</p> : null}
            {modelBusy ? <p role="status" className="text-sm text-muted-foreground">{t("account.modelsLoading")}</p> : null}
            {modelError ? <p role="alert" className="text-sm text-destructive">{t("account.modelsFailed")}</p> : null}
            {providers.flatMap((p) => p.catalog).length ? <p className="break-words text-sm">{providers.flatMap((p) => p.catalog).join(" · ")}</p> : null}
          </section>
          <section className="flex flex-col gap-2">
            <h2 className="text-sm font-semibold">{t("account.pcs")}</h2>
            {!account.hosts.length ? <p className="text-sm text-muted-foreground">{t("account.empty")}</p> : <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border">
              {account.hosts.map((host) => <li key={host.id} className="flex items-center gap-2 p-3">
                <Monitor className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                <div className="min-w-0 flex-1"><p className="truncate text-sm font-medium">{host.name || host.hostname || host.id}</p><p className="text-xs text-muted-foreground">{host.online ? t("account.online") : t("account.offline")}</p></div>
                <Button variant="outline" disabled={!host.online || !host.enabled || Boolean(connecting)} onClick={() => void connect(host.id)} aria-label={`${t("account.connect")} ${host.name || host.hostname || host.id}`}>
                  {connecting === host.id ? <Loader2 className="size-4 animate-spin motion-reduce:animate-none" aria-hidden /> : null}
                  {connecting === host.id ? t("account.connecting") : t("account.connect")}
                </Button>
              </li>)}
            </ul>}
            {connectionError ? <p role="alert" className="text-sm text-destructive">{connectionError}</p> : null}
          </section>
        </> : null}
        {(authorized || waiting) ? <Button variant="outline" disabled={account.busy} onClick={() => void onRefresh()}>{t("account.retry")}</Button> : null}
        {(account.session || waiting) ? <Button variant="ghost" onClick={() => void account.logout()}>{t("account.logout")}</Button> : null}
        {!waiting ? <>
          <Button variant="ghost" onClick={() => setAdvanced((value) => !value)} aria-expanded={advanced}>{t("account.advanced")}</Button>
          {advanced ? <form className="flex flex-col gap-2" onSubmit={(event) => { event.preventDefault(); setConnectionError(""); try { account.setOrigin(server) } catch { setConnectionError(t("chat.badUrl")) } }}>
            <label htmlFor="account-server" className="text-sm">{t("account.server")}</label>
            <Input id="account-server" value={server} onChange={(event) => setServer(event.target.value)} autoCapitalize="off" autoCorrect="off" spellCheck={false} />
            <p className="text-xs text-muted-foreground">{t("account.serverHint")}</p>
            <Button variant="outline" type="submit">{t("account.saveServer")}</Button>
          </form> : null}
        </> : null}
      </div>
    </main>
  )
}
