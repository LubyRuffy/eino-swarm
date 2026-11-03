import { openSaved, type RPCOptions } from "./client"
import { mockLink, wantsMock } from "./mock-link"
import type { RemoteRequest, RemoteResponse } from "./rpc"
import type { SavedLink } from "./store"
import { gatewayLinkForReconnect } from "./gateway-account"
import { gatewayUpgradePending } from "./gateway-link-upgrade"

/** Everything the app asks of a PC. The product link is a sealed WebSocket
 *  (`DeviceLink`); the walkthrough link is scripted and offline. */
export type RemoteLink = {
  path: string
  webExecutionSupported?: boolean
  alive(): boolean
  rpc(
    req: Omit<RemoteRequest, "v" | "id"> & { id?: string },
    opts?: RPCOptions,
  ): Promise<RemoteResponse>
  close(): void
  announceDevice(label?: string): Promise<RemoteResponse | undefined>
  onPush?: (resp: RemoteResponse) => void
  onDisconnect?: (err: Error) => void
}

export async function openLink(saved: SavedLink): Promise<RemoteLink> {
  if (wantsMock()) return mockLink()
  if (gatewayUpgradePending(saved)) throw new Error("pc_upgrade_pending")
  return openSaved(saved.gatewayOwnerID ? gatewayLinkForReconnect(saved) : saved)
}
