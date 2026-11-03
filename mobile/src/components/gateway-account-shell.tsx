import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react"
import { GatewayAccountScreen } from "@/components/gateway-account-screen"
import type { PcWebSnapshot } from "@/components/pc-web-execution-status"
import { installAndroidBack } from "@/lib/android-back"
import type { DirectProvider } from "@/lib/direct-provider"
import { t } from "@/lib/i18n"
import { gatewayProvider } from "@/lib/gateway-models"
import { discoverModels } from "@/lib/openai-client"
import { useGatewayAccount } from "@/lib/use-gateway-account"

type Account = ReturnType<typeof useGatewayAccount>
type PhoneAccount = { providers: DirectProvider[]; openAccount?: (pcWeb?: PcWebSnapshot) => void; gatewaySourceID?: string; empty?: ReactNode }
const PhoneAccountContext = createContext<PhoneAccount>({ providers: [] })
export const usePhoneAccount = () => useContext(PhoneAccountContext)

export function GatewayAccountShell({ children }: { children: ReactNode }) {
  const account = useGatewayAccount()
  const [dialog, setDialog] = useState<{ pcWeb?: PcWebSnapshot }>()
  const open = Boolean(dialog)
  const [catalog, setCatalog] = useState<{ authority: string; names: string[] }>({ authority: "", names: [] })
  const [modelError, setModelError] = useState(false)
  const [modelBusy, setModelBusy] = useState(false)
  const [catalogRevision, setCatalogRevision] = useState(0)
  const source = gatewayProvider(account.status === "authorized" ? account.session : null, account.bootstrap ?? null)
  const authority = source ? source.id + "\x00" + source.apiKey : ""
  useEffect(() => {
    let active = true
    setModelError(false)
    setModelBusy(Boolean(source))
    if (!source) return () => { active = false }
    void discoverModels(source).then((names) => {
      if (active) setCatalog({ authority, names })
    }).catch(() => {
      if (active) { setCatalog({ authority, names: [] }); setModelError(true) }
    }).finally(() => { if (active) setModelBusy(false) })
    return () => { active = false }
  }, [authority, catalogRevision])
  const providers = useMemo(() => source && catalog.authority === authority && catalog.names.length
    ? [{ ...source, catalog: catalog.names, model: catalog.names[0] }]
    : [], [authority, catalog, source?.label])
  const restricted = ["checking", "authorization_pending", "account_pending", "account_rejected"].includes(account.status) || Boolean(account.session && account.status === "access_denied")
  useEffect(() => {
    if (!open || restricted) return
    return installAndroidBack(() => { setDialog(undefined); return true })
  }, [open, restricted])
  const refresh = async () => { await account.refresh(); setCatalogRevision((value) => value + 1) }
  const connect = async (id: string) => {
    const saved = await account.connectHost(id)
    setDialog(undefined)
    window.dispatchEvent(new CustomEvent("zwai.gateway.connect", { detail: saved.fingerprint }))
  }
  const openChat = () => { setDialog(undefined); window.dispatchEvent(new Event("zwai.gateway.chat")) }
  const screen = <GatewayAccountScreen account={account} providers={providers} modelBusy={modelBusy}
    modelError={modelError} pcWeb={dialog?.pcWeb} onClose={restricted ? undefined : () => setDialog(undefined)} onConnect={connect} onRefresh={refresh} onOpenChat={openChat} />
  return (
    <PhoneAccountContext.Provider value={{ providers, gatewaySourceID: account.status === "authorized" ? account.bootstrap?.models.source_id ?? "" : "", openAccount: (pcWeb) => setDialog({ pcWeb }), empty: account.status === "authorized" ? <GatewayAccountScreen account={account} providers={providers} modelBusy={modelBusy} modelError={modelError} onConnect={connect} onRefresh={refresh} onOpenChat={openChat} /> : undefined }}>
      {restricted ? screen : <div className="h-full min-h-0" inert={open || undefined} aria-hidden={open || undefined}>{children}</div>}
      {open && !restricted ? <div role="dialog" aria-modal="true" aria-label={t("account.title")} className="fixed inset-0 z-50 bg-background pl-[env(safe-area-inset-left)] pr-[env(safe-area-inset-right)] pt-[env(safe-area-inset-top)]">{screen}</div> : null}
    </PhoneAccountContext.Provider>
  )
}

export type GatewayAccountController = Account
