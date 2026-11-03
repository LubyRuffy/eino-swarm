import { useCallback, useSyncExternalStore } from "react"

import { api } from "./api"
import { RemoteStatusOwner, remoteAuthorityKey } from "./remote-status-owner"
import type { RemoteSettings, RemoteStatus } from "./types"

export { REMOTE_STATUS_WATCH_MS } from "./remote-status-owner"

const owner = new RemoteStatusOwner((signal) => api.remoteStatus(signal))

export function remoteStatusMatches(status: RemoteStatus, remote: RemoteSettings) {
  return remoteAuthorityKey(status) === remoteAuthorityKey(remote)
}

/** The rail and account dialog subscribe outside App's transcript state. */
export function useDesktopAccount(authority?: string) {
  const subscribe = useCallback((listener: () => void) => {
    if (authority !== undefined) owner.configurationChanged(authority)
    return owner.subscribe(listener)
  }, [authority])
  const snapshot = useSyncExternalStore(subscribe, owner.getSnapshot)
  return { ...snapshot, refresh: owner.refresh, perform: owner.perform }
}

/** Phone drafts can lead the saved configuration. Only its exact status echo
 * enables actions, while all surfaces share read and mutation ownership. */
export function useRemoteStatus(remote: RemoteSettings) {
  const key = remoteAuthorityKey(remote)
  const connection = useDesktopAccount(key)
  const status = connection.status && remoteStatusMatches(connection.status, remote)
    ? connection.status : undefined
  return { ...connection, status }
}
