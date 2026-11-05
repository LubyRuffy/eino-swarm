import { useState } from "react"
import { Loader2, Monitor } from "lucide-react"

import { Button } from "@/components/ui/button"
import { GatewayError, type GatewayHost } from "@/lib/gateway-account"
import { gatewayHostDisplayName } from "@/lib/gateway-names"
import { t } from "@/lib/i18n"

/** The computers signed in to this account, one Connect action each. Shared by
 *  the account page and the Add a PC sheet so both explain a refusal the same
 *  way instead of leaving the person at a dead button. */
export function AccountPcList({ hosts, onConnect }: {
  hosts: GatewayHost[]
  onConnect: (id: string) => Promise<void>
}) {
  const [connecting, setConnecting] = useState("")
  const [error, setError] = useState("")
  const connect = async (id: string) => {
    setConnecting(id)
    setError("")
    try {
      await onConnect(id)
    } catch (failure) {
      setError(t(failure instanceof GatewayError && [401, 403].includes(failure.status) ? "account.expired" : failure instanceof GatewayError && failure.status === 409 ? "account.hostOffline" : "account.connectionFailed"))
    } finally { setConnecting("") }
  }
  return (
    <>
      <ul className="divide-y divide-border overflow-hidden rounded-xl border border-border">
        {hosts.map((host) => {
          const name = gatewayHostDisplayName(host) || t("account.unnamedPC")
          return <li key={host.id} className="flex items-center gap-2 p-3">
            <Monitor className="size-4 shrink-0 text-muted-foreground" aria-hidden />
            <div className="min-w-0 flex-1"><p className="truncate text-sm font-medium">{name}</p><p className="text-xs text-muted-foreground">{host.online ? t("account.online") : t("account.offline")}</p></div>
            <Button variant="outline" disabled={!host.online || !host.enabled || Boolean(connecting)} onClick={() => void connect(host.id)} aria-label={`${t("account.connect")} ${name}`}>
              {connecting === host.id ? <Loader2 className="size-4 animate-spin motion-reduce:animate-none" aria-hidden /> : null}
              {connecting === host.id ? t("account.connecting") : t("account.connect")}
            </Button>
          </li>
        })}
      </ul>
      {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
    </>
  )
}
