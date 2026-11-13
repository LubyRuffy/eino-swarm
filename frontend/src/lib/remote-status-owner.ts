import type { RemoteStatus } from "./types"

export const REMOTE_STATUS_WATCH_MS = 1000

export function remoteAuthorityKey(remote: { enabled: boolean; hub_url: string; auth_mode?: string }) {
  return JSON.stringify([remote.enabled, remote.hub_url.trim().replace(/\/+$/, ""), remote.auth_mode ?? "account"])
}

export type RemoteStatusSnapshot = {
  status?: RemoteStatus
  unavailable: boolean
  busy: boolean
}

/** One local status reader and mutation fence for every desktop account surface.
 * Go owns login work; dropping the last renderer subscriber only cancels reads. */
export class RemoteStatusOwner {
  private snapshot: RemoteStatusSnapshot = { unavailable: false, busy: false }
  private listeners = new Set<() => void>()
  private timer?: ReturnType<typeof setInterval>
  private read?: AbortController
  private epoch = 0
  private mutating = false
  private configuration?: string
  private expectedConfiguration?: string

  constructor(private readonly fetchStatus: (signal: AbortSignal) => Promise<RemoteStatus>) {}

  getSnapshot = () => this.snapshot

  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    if (this.listeners.size === 1) {
      // Batch initial subscribers so a Phone surface can declare its saved
      // authority before the first read, without a second competing poll.
      const epoch = this.epoch
      queueMicrotask(() => { if (epoch === this.epoch) void this.refresh() })
      this.timer = setInterval(() => {
        if (!document.hidden) void this.refresh()
      }, REMOTE_STATUS_WATCH_MS)
    }
    return () => {
      this.listeners.delete(listener)
      if (this.listeners.size) return
      clearInterval(this.timer)
      this.timer = undefined
      this.epoch++
      this.read?.abort()
      this.read = undefined
      this.configuration = undefined
      this.expectedConfiguration = undefined
      this.snapshot = { unavailable: false, busy: this.mutating }
    }
  }

  private publish(next: RemoteStatusSnapshot) {
    // Preserve snapshot identity when the bounded, public local receipt is
    // unchanged, so the one-second watch cannot repaint either surface.
    if (JSON.stringify(next) === JSON.stringify(this.snapshot)) return
    this.snapshot = next
    for (const listener of this.listeners) listener()
  }

  private accept(status: RemoteStatus, mutation = false) {
    const key = remoteAuthorityKey(status)
    if (mutation) {
      // An explicit enable-account receipt establishes its new configuration.
      this.expectedConfiguration = key
    } else {
      if (this.expectedConfiguration && this.expectedConfiguration !== key) return
      // Once the declared configuration is observed, ordinary polling can also
      // discover later external configuration changes.
      this.expectedConfiguration = undefined
    }
    this.configuration = key
    this.publish({ status, unavailable: false, busy: this.mutating })
  }

  configurationChanged(key: string) {
    if (this.configuration === key) return
    const previous = this.configuration
    this.configuration = key
    this.expectedConfiguration = key
    if (previous === undefined && !this.read && !this.mutating) return
    this.epoch++
    this.read?.abort()
    this.read = undefined
    this.publish({ unavailable: false, busy: this.mutating })
    void this.refresh()
  }

  refresh = async () => {
    if (!this.listeners.size || this.read || this.mutating) return
    const controller = new AbortController()
    this.read = controller
    const epoch = this.epoch
    try {
      const status = await this.fetchStatus(controller.signal)
      if (this.listeners.size && epoch === this.epoch && !controller.signal.aborted) this.accept(status)
    } catch {
      if (this.listeners.size && epoch === this.epoch && !controller.signal.aborted) {
        this.publish({ ...this.snapshot, unavailable: true })
      }
    } finally {
      if (this.read === controller) this.read = undefined
    }
  }

  perform = async (action: () => Promise<RemoteStatus>) => {
    if (this.mutating) return
    this.mutating = true
    const epoch = ++this.epoch
    this.read?.abort()
    this.read = undefined
    this.publish({ ...this.snapshot, busy: true })
    try {
      const status = await action()
      if (this.listeners.size && epoch === this.epoch) this.accept(status, true)
    } catch (error) {
      if (this.listeners.size && epoch === this.epoch) throw error
    } finally {
      this.mutating = false
      this.publish({ ...this.snapshot, busy: false })
      if (epoch !== this.epoch) void this.refresh()
    }
  }
}
