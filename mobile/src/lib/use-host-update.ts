import { App } from "@capacitor/app"
import { Capacitor } from "@capacitor/core"
import { useCallback, useEffect, useReducer, useRef } from "react"

import type { RemoteLink } from "./link"
import { clearPendingHostUpdate, getPendingHostUpdate, newHostUpdateID, releaseVersionPattern, savePendingHostUpdate } from "./host-update-storage"
import { sameLinkAuthority } from "./link-authority"
import { usePhoneVisibility } from "./phone-visibility"
import {
  OpHostUpdateCheck, OpHostUpdateStart, OpHostUpdateStatus,
  type EngineStartup, type HostUpdateProgress,
} from "./rpc"
import type { SavedLink } from "./store"

type Scope = { owner: SavedLink; link: RemoteLink }
type Offer = Scope & { status: "available"; current: string; version: string }
type Check = Offer | { status: "checking" | "current" | "unsupported" | "error"; message?: string }
type Attempt = Omit<HostUpdateProgress, "stage"> & {
  stage: HostUpdateProgress["stage"] | "sending" | "unconfirmed" | "missing"
  current: string
}
type Record = {
  owner: SavedLink
  check?: Check
  manual?: boolean
  dismissed?: string
  hiddenAttempt?: { id: string; stage: Attempt["stage"] }
  attempt?: Attempt
}

const readOptions = { dropOnTimeout: false, timeoutMs: 5000 }
const stages: ReadonlySet<string> = new Set([
  "checking", "downloading", "installing", "saving", "restarting", "restoring", "completed", "failed", "pending",
])

function matchesProgress(progress: HostUpdateProgress | undefined, attempt: Attempt): progress is HostUpdateProgress {
  return Boolean(progress && progress.operation_id === attempt.operation_id && progress.version === attempt.version && stages.has(progress.stage))
}

function terminal(attempt: Attempt) {
  return attempt.stage === "failed" || attempt.stage === "completed"
}

/** Discovery and readback follow the selected authority. Only an explicit
 *  confirmation or retry starts an install. Polling acknowledges a candidate
 *  challenge for that confirmed operation on the same paired link. */
export function useHostUpdate(owner: SavedLink | undefined, link: RemoteLink | null, startup: EngineStartup | undefined,
  acceptsLink: (owner: SavedLink, link: RemoteLink) => boolean) {
  const records = useRef(new Map<string, Record>())
  const [, redraw] = useReducer((n: number) => n + 1, 0)
  const latest = useRef({ owner, link, startup, acceptsLink })
  latest.current = { owner, link, startup, acceptsLink }
  const mounted = useRef(false)
  const checking = useRef<{ scope: Scope; manual: boolean } | undefined>(undefined)
  const submitting = useRef(new Set<string>())
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false; checking.current = undefined }
  }, [])

  const record = useCallback((target: SavedLink) => {
    const found = records.current.get(target.fingerprint)
    return found && sameLinkAuthority(found.owner, target) ? found : undefined
  }, [])
  const change = useCallback((target: SavedLink, update: (previous: Record) => Record) => {
    if (!mounted.current) return
    const previous = record(target) ?? { owner: target }
    const next = update(previous)
    if (previous === next) return
    records.current.set(target.fingerprint, next)
    redraw()
  }, [record])
  const current = useCallback((scope: Scope) => {
    const active = latest.current
    return mounted.current && active.link === scope.link && sameLinkAuthority(active.owner, scope.owner) &&
      scope.link.alive() && active.acceptsLink(scope.owner, scope.link)
  }, [])

  useEffect(() => {
    if (!owner || record(owner)?.attempt) return
    const pending = getPendingHostUpdate(owner)
    if (pending) change(owner, previous => ({ ...previous, attempt: { ...pending, stage: "unconfirmed" } }))
  }, [owner, change, record])

  const check = useCallback(async (manual: boolean) => {
    const { owner, link, startup } = latest.current
    if (!owner || !link || !current({ owner, link })) return
    const scope = { owner, link }
    const attempt = record(owner)?.attempt
    if (attempt && !terminal(attempt)) {
      if (manual) change(owner, previous => ({ ...previous, hiddenAttempt: undefined }))
      return
    }
    if (startup?.startup !== "ready" || !startup.host_update_supported) {
      if (manual) change(owner, previous => ({ ...previous, check: { status: "unsupported" }, manual: true, attempt: undefined }))
      return
    }
    if (document.hidden && !manual) return
    const running = checking.current
    if (running && current(running.scope)) {
      if (manual && !running.manual) {
        running.manual = true
        change(owner, previous => ({ ...previous, manual: true, check: previous.check ?? { status: "checking" } }))
      }
      return
    }
    const job = { scope, manual }
    checking.current = job
    if (manual) {
      if (attempt) clearPendingHostUpdate(owner, attempt.operation_id)
      change(owner, previous => ({ ...previous, check: { status: "checking" }, manual: true, attempt: undefined }))
    }
    let result: Check = { status: "error" }
    try {
      const response = await link.rpc({ op: OpHostUpdateCheck, ...(manual ? { fresh: true } : {}) }, {
        ...readOptions, timeoutMs: 35_000,
      })
      const offer = response.host_update_offer
      if (response.ok && offer?.status === "available" && offer.current && offer.offer &&
        releaseVersionPattern.test(offer.current) && releaseVersionPattern.test(offer.offer.version)) {
        result = { status: "available", ...scope, current: offer.current, version: offer.offer.version }
      } else if (response.ok && offer?.status === "current") {
        result = { status: "current" }
      } else if (response.code === "unknown_op" || (response.ok && offer?.status === "unsupported")) {
        result = { status: "unsupported" }
      } else {
        result = { status: "error", message: offer?.message || response.error }
      }
    } catch {
      // Checking never disconnects the transport or retries an installation.
    } finally {
      if (checking.current === job) {
        checking.current = undefined
        if (current(scope)) change(owner, previous => {
          if (previous.attempt && !terminal(previous.attempt)) return previous
          const prior = previous.check
          // Foreground checks can land while the confirmation sheet is open.
          // An identical offer in that exact binding still authorizes the same version.
          const same = prior?.status === "available" && result.status === "available" && prior.link === result.link &&
            prior.current === result.current && prior.version === result.version
          return { ...previous, check: same ? prior : result, manual: previous.manual || job.manual }
        })
      }
    }
  }, [change, current, record])

  useEffect(() => { void check(false) }, [link, owner, startup?.startup, startup?.host_update_supported, startup?.host_version, check])
  usePhoneVisibility(() => { void check(false) })
  useEffect(() => {
    if (Capacitor.getPlatform() === "web") return
    let live = true
    const handle = App.addListener("appStateChange", event => { if (live && event.isActive) void check(false) })
    return () => { live = false; void handle.then(value => value.remove()).catch(() => undefined) }
  }, [check])

  const submit = async (scope: Scope, attempt: Attempt) => {
    if (!current(scope) || submitting.current.has(scope.owner.fingerprint)) return
    const { owner, link } = scope
    const id = attempt.operation_id
    submitting.current.add(owner.fingerprint)
    savePendingHostUpdate(owner, attempt)
    change(owner, previous => ({ ...previous, attempt, manual: false, hiddenAttempt: undefined }))
    let next: Attempt = { ...attempt, stage: "unconfirmed" }
    try {
      const response = await link.rpc({ op: OpHostUpdateStart, version: attempt.version, operation_id: id, host_update_verify: true }, readOptions)
      if (response.ok && matchesProgress(response.host_update, attempt)) {
        next = { ...response.host_update, current: attempt.current }
      } else if (!response.ok) {
        next = { ...attempt, stage: "failed", error: response.error }
      }
    } catch {
      // A missing acknowledgement can still correspond to a live installation.
    } finally {
      submitting.current.delete(owner.fingerprint)
      if (current(scope)) change(owner, previous => previous.attempt?.operation_id === id && previous.attempt.stage === "sending"
        ? { ...previous, attempt: next } : previous)
    }
  }

  const saved = owner ? record(owner) : undefined
  const attempt = saved?.attempt
  const operationID = attempt?.operation_id
  const observing = Boolean(attempt && !terminal(attempt) && attempt.stage !== "missing")
  useEffect(() => {
    if (!owner || !link || !operationID || !observing) return
    const scope = { owner, link }
    const abort = new AbortController()
    let reading = false
    let challenge: string | undefined
    const poll = async () => {
      const request = record(owner)?.attempt
      if (reading || !current(scope) || document.hidden || submitting.current.has(owner.fingerprint) ||
        request?.operation_id !== operationID || terminal(request)) return
      reading = true
      try {
        const response = await link.rpc({ op: OpHostUpdateStatus, operation_id: operationID,
          ...(challenge ? { host_update_challenge: challenge } : {}) }, { ...readOptions, signal: abort.signal })
        if (abort.signal.aborted || !current(scope)) return
        const progress = response.host_update
        if (response.ok && matchesProgress(progress, request) && response.host_version === request.version &&
          /^[0-9a-f]{64}$/.test(response.host_update_challenge ?? "")) {
          challenge = response.host_update_challenge
        }
        change(owner, previous => {
          if (previous.attempt !== request) return previous
          if (response.ok && matchesProgress(progress, request)) return { ...previous, attempt: { ...progress, current: request.current } }
          if (response.code === "not_found" && (request.stage === "sending" || request.stage === "unconfirmed")) {
            return { ...previous, attempt: { ...request, stage: "missing" } }
          }
          return previous
        })
      } catch {
        // Keep the last observed state until the same authority reconnects.
      } finally { reading = false }
    }
    void poll()
    const timer = window.setInterval(() => void poll(), 1000)
    return () => { abort.abort(); window.clearInterval(timer) }
  }, [owner, link, operationID, observing, change, current, record])

  const found = saved?.check
  const offer = found?.status === "available" && found.link === link && found.current === startup?.host_version &&
    (saved?.manual || saved?.dismissed !== found.version) ? found : undefined
  const canStart = Boolean(owner && link && startup?.startup === "ready" && startup.host_update_supported && current({ owner, link }))
  const completed = attempt?.stage === "completed" && startup?.startup === "ready" && startup.host_version === attempt.version && Boolean(link?.alive())
  const attemptStage = attempt?.stage === "completed" && !completed ? "restoring" : attempt?.stage
  const retry = () => {
    if (!owner || !link || !canStart || !attempt || record(owner)?.attempt !== attempt ||
      (attempt.stage !== "failed" && attempt.stage !== "missing")) return
    const id = attempt.stage === "missing" ? attempt.operation_id : newHostUpdateID()
    void submit({ owner, link }, { operation_id: id, current: attempt.current, version: attempt.version, stage: "sending" })
  }
  const confirm = (target: Offer) => {
    if (!canStart || offer !== target || !current(target) || record(target.owner)?.check !== target) return
    const active = record(target.owner)?.attempt
    if (active && !terminal(active)) return
    void submit(target, { operation_id: newHostUpdateID(), current: target.current, version: target.version, stage: "sending" })
  }
  const dismiss = () => {
    if (!owner) return
    const closed = record(owner)?.attempt
    if (closed && terminal(closed)) clearPendingHostUpdate(owner, closed.operation_id)
    change(owner, previous => ({ ...previous, manual: false,
      dismissed: offer?.version ?? previous.dismissed,
      hiddenAttempt: closed ? { id: closed.operation_id, stage: closed.stage } : previous.hiddenAttempt,
    }))
  }
  const hidden = saved?.hiddenAttempt
  return { offer, attempt: hidden?.id === operationID && hidden?.stage === attemptStage ? undefined : attempt, attemptStage, canStart,
    check: saved?.manual ? saved.check : undefined, checkNow: () => void check(true), confirm, dismiss, retry,
  }
}

export type HostUpdateControl = ReturnType<typeof useHostUpdate>
