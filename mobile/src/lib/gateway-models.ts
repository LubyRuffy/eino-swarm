import { defaultTimeoutSeconds, type DirectProvider } from "./direct-provider"
import type { GatewayBootstrap, GatewaySession } from "./gateway-account"
import { t } from "./i18n"

/** Account source identity belongs to the gateway/account, never a model name.
 * The credential lives only in this ephemeral projection. */
export function gatewayProvider(session: GatewaySession | null, bootstrap: GatewayBootstrap | null): DirectProvider | null {
  if (!session || !bootstrap?.models.api_access_enabled || !bootstrap.models.source_id.startsWith("gateway:")) return null
  return {
    id: bootstrap.models.source_id,
    sourceKind: "gateway",
    label: t("account.serverSource", { name: bootstrap.models.label }),
    baseURL: bootstrap.models.api_base_url,
    apiKey: session.token,
    api: "chat",
    model: "",
    catalog: [],
    timeoutSeconds: defaultTimeoutSeconds,
    enabled: true,
  }
}
