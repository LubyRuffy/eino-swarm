import { useEffect, useState } from "react"
import { X } from "lucide-react"
import { AccountPcList } from "@/components/account-pc-list"
import type { GatewayAccountController } from "@/components/gateway-account-shell"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import type { DirectProvider } from "@/lib/direct-provider"
import { acceptAvatarURL } from "@/lib/avatar-url"
import { t } from "@/lib/i18n"
import { gatewayUserDisplayName } from "@/lib/gateway-names"
import { WebExecutionSettings } from "@/components/web-execution-settings"
import type { PcWebSnapshot } from "@/components/pc-web-execution-status"

function AccountPortrait({ name, url }: { name: string; url?: string }) {
  const picture = acceptAvatarURL(url)
  const [failed, setFailed] = useState(false)
  useEffect(() => { setFailed(false) }, [picture])
  return (
    <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-full bg-muted text-sm font-medium">
      {picture && !failed ? <img src={picture} alt="" referrerPolicy="no-referrer" className="size-full object-cover" onError={() => setFailed(true)} /> : Array.from(name)[0]}
    </span>
  )
}

export function GatewayAccountScreen({ account, providers, modelBusy, modelError, pcWeb, onClose, onConnect, onRefresh, onOpenChat, onOtherWays }: {
  account: GatewayAccountController
  providers: DirectProvider[]
  modelBusy: boolean
  modelError: boolean
  pcWeb?: PcWebSnapshot
  onClose?: () => void
  onOpenChat?: () => void
  /** Present on the signed-in start page, where pairing by QR and a model
   *  service of one's own are the secondary ways in. */
  onOtherWays?: { scan: () => void; addModel: () => void }
  onConnect: (id: string) => Promise<void>
  onRefresh: () => Promise<void>
}) {
  const [formError, setFormError] = useState("")
  const [otherWays, setOtherWays] = useState(false)
  const [advanced, setAdvanced] = useState(false)
  const [server, setServer] = useState(account.origin)
  const authorized = account.status === "authorized"
  const accountName = account.session ? gatewayUserDisplayName(account.session.user) : ""
  const waiting = ["checking", "authorization_pending", "account_pending", "account_rejected"].includes(account.status) || Boolean(account.session && account.status === "access_denied")
  const statusText = account.status === "account_pending" ? t("account.pending")
    : account.status === "account_rejected" ? t("account.rejected")
    : account.status === "authorization_pending" ? t("account.browser")
    : account.status === "checking" ? t("account.checking")
    : account.status === "access_denied" ? t(account.session ? "account.accessDenied" : "account.denied")
    : account.status === "expired" ? t("account.expired") : ""
  return (
    <main data-testid="gateway-account" className="mx-auto flex h-full max-w-lg flex-col overflow-y-auto pb-[env(safe-area-inset-bottom)]">
      <header className="flex h-12 shrink-0 items-center border-b border-border px-4">
        <h1 className="flex-1 text-base font-semibold">{t("account.title")}</h1>
        {onClose ? <Button variant="ghost" className="size-10 px-0" aria-label={t("home.close")} onClick={onClose}><X className="size-5" aria-hidden /></Button> : null}
      </header>
      <div className="flex flex-col gap-4 p-5">
        {account.session ? <div className="flex items-center gap-3">
          <AccountPortrait name={accountName} url={account.session.user.avatar_url} />
          <p className="break-all text-sm font-medium">{accountName}</p>
        </div> : null}
        {statusText ? <p role="status" className="text-sm text-muted-foreground">{statusText}</p> : null}
        {account.error ? <p role="alert" className="text-sm text-destructive">{t(account.error === "browser_unavailable" ? "account.browserFailed" : account.error === "gateway_timeout" ? "account.timeout" : account.error === "pc_upgrade_failed" ? "account.upgradeFailed" : "chat.failed")}</p> : null}
        {!authorized && (!waiting || account.status === "access_denied") ? <>
          <p className="text-sm leading-relaxed text-muted-foreground">{t("account.hint")}</p>
          <Button disabled={account.busy} onClick={() => void account.login()}>{t("account.login")}</Button>
          {onClose ? <Button variant="ghost" onClick={onClose}>{t("account.anonymous")}</Button> : null}
        </> : null}
        {!waiting ? <WebExecutionSettings pcWeb={pcWeb} /> : null}
        {authorized ? <>
          <section className="flex flex-col gap-2">
            <h2 className="text-sm font-semibold">{t("account.models")}</h2>
            <p className="text-xs text-muted-foreground">{t("account.modelsHint")}</p>
            {onOpenChat ? <Button variant="outline" onClick={onOpenChat}>{t("account.history")}</Button> : null}
            {!account.bootstrap?.models.api_access_enabled ? <p className="text-sm text-muted-foreground">{t("account.modelsDenied")}</p> : null}
            {modelBusy ? <p role="status" className="text-sm text-muted-foreground">{t("account.modelsLoading")}</p> : null}
            {modelError ? <p role="alert" className="text-sm text-destructive">{t("account.modelsFailed")}</p> : null}
            {providers.flatMap((p) => p.catalog).length ? <p className="wrap-break-word text-sm">{providers.flatMap((p) => p.catalog).join(" · ")}</p> : null}
          </section>
          <section className="flex flex-col gap-2">
            <h2 className="text-sm font-semibold">{t("account.pcs")}</h2>
            {!account.hosts.length ? <p className="text-sm text-muted-foreground">{t("account.empty")}</p> : <AccountPcList hosts={account.hosts} onConnect={onConnect} />}
          </section>
          {onOtherWays ? <section className="flex flex-col gap-2">
            <Button variant="ghost" aria-expanded={otherWays} aria-controls="account-other-ways" onClick={() => setOtherWays((value) => !value)}>{t("welcome.other")}</Button>
            {otherWays ? <div id="account-other-ways" className="flex flex-col gap-2">
              <p className="text-xs text-muted-foreground">{t("welcome.otherHint")}</p>
              <Button variant="outline" onClick={onOtherWays.scan}>{t("home.addHost")}</Button>
              <Button variant="outline" onClick={onOtherWays.addModel}>{t("chat.connect")}</Button>
            </div> : null}
          </section> : null}
        </> : null}
        {(authorized || waiting) ? <Button variant="outline" disabled={account.busy} onClick={() => void onRefresh()}>{t("account.retry")}</Button> : null}
        {(account.session || waiting) ? <Button variant="ghost" onClick={() => void account.logout()}>{t("account.logout")}</Button> : null}
        {!waiting ? <>
          <Button variant="ghost" onClick={() => setAdvanced((value) => !value)} aria-expanded={advanced}>{t("account.advanced")}</Button>
          {advanced ? <form className="flex flex-col gap-2" onSubmit={(event) => { event.preventDefault(); setFormError(""); try { account.setOrigin(server) } catch { setFormError(t("chat.badUrl")) } }}>
            <label htmlFor="account-server" className="text-sm">{t("account.server")}</label>
            <Input id="account-server" value={server} onChange={(event) => setServer(event.target.value)} autoCapitalize="off" autoCorrect="off" spellCheck={false} />
            <p className="text-xs text-muted-foreground">{t("account.serverHint")}</p>
            <Button variant="outline" type="submit">{t("account.saveServer")}</Button>
          </form> : null}
          {formError ? <p role="alert" className="text-sm text-destructive">{formError}</p> : null}
        </> : null}
      </div>
    </main>
  )
}
