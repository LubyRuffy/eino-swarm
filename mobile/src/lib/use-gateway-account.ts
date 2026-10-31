import { useCallback, useEffect, useRef, useState } from "react"
import { App } from "@capacitor/app"
import {
  clearGatewayPending, clearGatewaySession, dropGatewayLinks, issueGatewayHostLink, GatewayError,
  getGatewayOrigin, loadGatewayPending, loadGatewaySession,
  logoutGatewaySession, pollGatewayLogin, readGatewaySession, refreshGatewaySession, saveGatewaySession,
  setGatewayOrigin, startGatewayLogin, normalizeGatewayOrigin,
  type GatewayBootstrap, type GatewayHost, type GatewayPending, type GatewayStatus,
} from "./gateway-account"
import { saveLink } from "./store"
import { usePhoneVisibility } from "./phone-visibility"

export function useGatewayAccount() {
  const [origin, putOrigin] = useState(getGatewayOrigin)
  const [session, putSession] = useState(loadGatewaySession)
  const [status, putStatus] = useState<GatewayStatus>(() => loadGatewaySession() ? "checking" : "signed_out")
  const [bootstrap, putBootstrap] = useState<GatewayBootstrap>()
  const [hosts, putHosts] = useState<GatewayHost[]>([])
  const [error, putError] = useState<string>()
  const [busy, putBusy] = useState(false)
  const generation = useRef(0)
  const active = useRef<AbortController | undefined>(undefined)
  const pollTimer = useRef(0)
  const pending = useRef<GatewayPending | null>(loadGatewayPending())
  const polling = useRef<GatewayPending | null>(null)
  const nextPoll = useRef(0)
  const selecting = useRef(0)
  const refreshing = useRef<number | null>(null)
  const pollRef = useRef<() => Promise<void>>(async () => undefined)

  const cancel = useCallback(() => {
    generation.current++
    active.current?.abort()
    window.clearTimeout(pollTimer.current)
    pending.current = null
    polling.current = null
    refreshing.current = null
    putBusy(false)
  }, [])

  const clearResources = useCallback(() => {
    putBootstrap(undefined)
    putHosts([])
  }, [])

  const failure = useCallback(async (err: unknown) => {
    if (err instanceof DOMException && err.name === "AbortError") return
    if (err instanceof GatewayError && (err.status === 401 || err.status === 403)) {
      cancel()
      selecting.current++
    }
    clearResources()
    if (err instanceof GatewayError && err.status === 401) {
      clearGatewaySession()
      putSession(null)
      putStatus("expired")
    } else {
      putStatus(err instanceof GatewayError && err.status === 403 ? "access_denied" : "error")
      putError(err instanceof GatewayError ? err.code : "gateway_unavailable")
      if (err instanceof GatewayError && err.status === 403) {
        dropGatewayLinks()
        const current = loadGatewaySession()
        if (!current) return
        const round = generation.current
        const restricted = { ...current, user: { ...current.user, approval_status: undefined } }
        saveGatewaySession(restricted)
        putSession(restricted)
        const controller = new AbortController()
        active.current = controller
        refreshing.current = round
        try {
          const checked = await readGatewaySession(current, fetch, controller.signal)
          if (round !== generation.current || controller.signal.aborted || loadGatewaySession()?.token !== current.token) return
          saveGatewaySession(checked)
          putSession(checked)
          if (checked.user.approval_status === "pending" || checked.user.approval_status === "rejected") putStatus(checked.user.approval_status === "pending" ? "account_pending" : "account_rejected")
        } catch { /* The account remains restricted until its state can be verified. */ }
        finally { if (refreshing.current === round) refreshing.current = null }
      }
    }
  }, [cancel, clearResources])

  const refresh = useCallback(async () => {
    if (pending.current) {
      if (Date.now() >= nextPoll.current) await pollRef.current()
      return
    }
    const current = loadGatewaySession()
    if (!current || refreshing.current === generation.current) return
    const round = generation.current
    refreshing.current = round
    const controller = new AbortController()
    active.current = controller
    try {
      const state = await refreshGatewaySession(current, fetch, controller.signal)
      if (round !== generation.current || controller.signal.aborted) return
      saveGatewaySession(state.session)
      putSession(state.session)
      putStatus(state.status)
      putBootstrap(state.bootstrap)
      putHosts(state.hosts)
      putError(undefined)
      if (state.status !== "authorized") {
        selecting.current++
        dropGatewayLinks()
      }
    } catch (err) {
      if (round === generation.current && !controller.signal.aborted) await failure(err)
    } finally { if (refreshing.current === round) refreshing.current = null }
  }, [failure])

  const poll = useCallback(async () => {
    const flow = pending.current
    if (!flow || polling.current === flow || Date.now() < nextPoll.current) return
    polling.current = flow
    window.clearTimeout(pollTimer.current)
    const round = generation.current
    const controller = new AbortController()
    active.current = controller
    nextPoll.current = Date.now() + flow.interval * 1000
    try {
      const result = await pollGatewayLogin(flow, fetch, controller.signal)
      if (round !== generation.current || pending.current !== flow || controller.signal.aborted) return
      putStatus(result.status)
      putError(undefined)
      if (result.session) {
        pending.current = null
        clearGatewayPending()
        saveGatewaySession(result.session)
        putSession(result.session)
        await refresh()
      } else if (result.status === "expired" || result.status === "access_denied") {
        pending.current = null
        clearGatewayPending()
      } else {
        pollTimer.current = window.setTimeout(() => { void pollRef.current() }, flow.interval * 1000)
      }
    } catch (err) {
      if (round !== generation.current || controller.signal.aborted) return
      if (err instanceof GatewayError && err.status === 429) {
        pollTimer.current = window.setTimeout(() => { void pollRef.current() }, flow.interval * 1000)
      } else {
        pending.current = null
        clearGatewayPending()
        await failure(err)
      }
    } finally { if (polling.current === flow) polling.current = null }
  }, [failure, refresh])
  pollRef.current = poll

  const login = useCallback(async () => {
    cancel()
    clearGatewaySession()
    clearResources()
    putSession(null)
    putError(undefined)
    putStatus("checking")
    putBusy(true)
    const round = generation.current
    const controller = new AbortController()
    active.current = controller
    try {
      const flow = await startGatewayLogin(getGatewayOrigin(), fetch, controller.signal)
      if (round !== generation.current || controller.signal.aborted) return
      pending.current = flow
      putStatus("authorization_pending")
      nextPoll.current = Date.now() + flow.interval * 1000
      pollTimer.current = window.setTimeout(() => { void pollRef.current() }, flow.interval * 1000)
    } catch (err) {
      if (round === generation.current && !controller.signal.aborted) {
        clearGatewayPending()
        await failure(err)
      }
    } finally { if (round === generation.current) putBusy(false) }
  }, [cancel, clearResources, failure])

  const logout = useCallback(async () => {
    cancel()
    const round = generation.current
    const current = loadGatewaySession()
    clearGatewaySession()
    clearResources()
    putSession(null)
    putStatus("signed_out")
    putError(undefined)
    if (current) {
      try { await logoutGatewaySession(current) } catch { if (round === generation.current) putError("logout_unconfirmed") }
    }
  }, [cancel, clearResources])

  const setOrigin = useCallback((raw: string) => {
    if (normalizeGatewayOrigin(raw) === getGatewayOrigin()) return
    setGatewayOrigin(raw)
    cancel()
    putOrigin(getGatewayOrigin())
    putSession(null)
    clearResources()
    putStatus("signed_out")
    putError(undefined)
  }, [cancel, clearResources])

  const connectHost = useCallback(async (id: string) => {
    const current = loadGatewaySession()
    if (!current || status !== "authorized") throw new GatewayError(401, "session_expired")
    const round = generation.current
    const selection = ++selecting.current
    const selected = hosts.find((host) => host.id === id)
    if (!selected?.enabled || !selected.online) throw new GatewayError(409, "pc_unavailable")
    const saved = await issueGatewayHostLink(current, id).catch(async (err: unknown) => {
      if (round === generation.current && selection === selecting.current && err instanceof GatewayError && (err.status === 401 || err.status === 403)) await failure(err)
      throw err
    })
    if (round !== generation.current || selection !== selecting.current) {
      throw new GatewayError(401, "session_expired")
    }
    const named = { ...saved, label: selected.name || selected.hostname }
    saveLink(named)
    return named
  }, [failure, hosts, status])

  useEffect(() => {
    if (pending.current) {
      putStatus("authorization_pending")
      nextPoll.current = Date.now() + pending.current.interval * 1000
      pollTimer.current = window.setTimeout(() => { void pollRef.current() }, pending.current.interval * 1000)
    } else { void refresh() }
    const interval = window.setInterval(() => { void refresh() }, 30_000)
    return () => {
      generation.current++
      active.current?.abort()
      window.clearTimeout(pollTimer.current)
      window.clearInterval(interval)
    }
  }, [refresh])

  usePhoneVisibility(() => { void refresh() })
  useEffect(() => {
    const listener = App.addListener("appStateChange", ({ isActive }) => { if (isActive) void refresh() })
    return () => { void listener.then((handle) => handle.remove()).catch(() => undefined) }
  }, [refresh])

  return { origin, session, status, bootstrap, hosts, error, busy, login, refresh, logout, setOrigin, connectHost }
}
