import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "./api"
import type { RemoteSettings, RemoteStatus } from "./types"

export const REMOTE_STATUS_WATCH_MS = 1000

function hub(value: string) { return value.trim().replace(/\/+$/, "") }

export function remoteStatusMatches(status: RemoteStatus, remote: RemoteSettings) {
  return status.enabled === remote.enabled && hub(status.hub_url) === hub(remote.hub_url)
    && (status.auth_mode ?? "legacy") === (remote.auth_mode ?? "legacy")
}

/** The local Go service owns the device proof, browser opening and Gateway polling.
 * This hook only watches its public state. Epochs reject reads from before a
 * mutation; the keyed Phone page isolates different saved Hub configurations. */
export function useRemoteStatus(remote: RemoteSettings) {
  const [status, setStatus] = useState<RemoteStatus>()
  const [unavailable, setUnavailable] = useState(false)
  const [busy, setBusy] = useState(false)
  const alive = useRef(false)
  const epoch = useRef(0)
  const inFlight = useRef<AbortController | undefined>(undefined)
  const mutation = useRef(false)
  const config = useRef(remote)
  config.current = remote

  const accept = useCallback((next: RemoteStatus) => {
    if (!remoteStatusMatches(next, config.current)) {
      setStatus(undefined)
      return
    }
    setStatus(next)
    setUnavailable(false)
  }, [])

  const refresh = useCallback(async () => {
    if (inFlight.current || mutation.current) return
    const controller = new AbortController()
    inFlight.current = controller
    const owner = epoch.current
    try {
      const next = await api.remoteStatus(controller.signal)
      if (alive.current && owner === epoch.current && !controller.signal.aborted) accept(next)
    } catch {
      if (alive.current && owner === epoch.current && !controller.signal.aborted) setUnavailable(true)
    } finally {
      if (inFlight.current === controller) inFlight.current = undefined
    }
  }, [accept])

  useEffect(() => {
    alive.current = true
    void refresh()
    const timer = remote.auth_mode === "nosec" ? window.setInterval(() => {
      if (!document.hidden) void refresh()
    }, REMOTE_STATUS_WATCH_MS) : undefined
    return () => {
      alive.current = false
      epoch.current++
      inFlight.current?.abort()
      inFlight.current = undefined
      window.clearInterval(timer)
    }
  }, [refresh, remote.auth_mode])

  const perform = async (action: () => Promise<RemoteStatus>) => {
    if (mutation.current) return
    mutation.current = true
    const owner = ++epoch.current
    inFlight.current?.abort()
    inFlight.current = undefined
    setBusy(true)
    try {
      const next = await action()
      if (alive.current && owner === epoch.current) accept(next)
    } catch (error) {
      if (alive.current && owner === epoch.current) throw error
    } finally {
      if (alive.current && owner === epoch.current) {
        mutation.current = false
        setBusy(false)
      }
    }
  }

  return { status, unavailable, busy, refresh, perform }
}
