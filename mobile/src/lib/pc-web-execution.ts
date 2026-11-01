import type { RemoteLink } from "./link"
import { OpMeta, type RemoteRequest } from "./rpc"
import { pcWebExecution } from "./web-execution"

/** An older PC must never silently ignore a requested execution location. */
export async function pcWebSelection(link: RemoteLink): Promise<Pick<RemoteRequest, "web_execution">> {
  if (link.webExecutionSupported === undefined) {
    const response = await link.rpc({ op: OpMeta }, { dropOnTimeout: false, timeoutMs: 5000 })
    if (!response.ok && response.code !== "unknown_op") throw new Error(response.error || response.code || "pc_metadata_unavailable")
    link.webExecutionSupported = response.ok && response.web_execution_supported === true
  }
  const value = pcWebExecution()
  if (!link.webExecutionSupported) {
    if (value) throw new Error("web_execution_unsupported")
    return {}
  }
  return { web_execution: value }
}
