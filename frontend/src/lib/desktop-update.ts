import type { DesktopUpdate, DesktopUpdateProgress } from "./api"

// Match the existing release cache so a long-lived window notices new builds
// without repeatedly reaching GitHub when the user switches applications.
export const DESKTOP_UPDATE_INTERVAL_MS = 6 * 60 * 60 * 1000

export type DesktopUpdatePhase =
  | { kind: "hidden" }
  | { kind: "checking" }
  | { kind: "current" }
  | { kind: "available"; version: string }
  | { kind: "working"; progress?: DesktopUpdateProgress }
  | { kind: "restarting" }
  | { kind: "unsupported"; message: string }
  | { kind: "error"; message: string; version?: string }

interface UpdateClient {
  check: (fresh: boolean) => Promise<DesktopUpdate>
  install: (version: string) => Promise<{ status: string }>
  progress: (signal: AbortSignal) => Promise<DesktopUpdateProgress>
  dismissedVersion: () => string
  dismissVersion: (version: string) => void
  onChange: (phase: DesktopUpdatePhase) => void
  now?: () => number
}

export class DesktopUpdateController {
  phase: DesktopUpdatePhase = { kind: "hidden" }
  private lastSuccess: number | null = null
  private sequence = 0
  private active: { manual: boolean } | null = null
  private disposed = false
  private progressTimer?: ReturnType<typeof setTimeout>
  private progressRequest?: AbortController

  constructor(private readonly client: UpdateClient) {}

  autoCheck() { return this.check(false) }

  checkManually() { return this.check(true) }

  get nextCheckDelay(): number {
    const elapsed = (this.client.now?.() ?? Date.now()) - (this.lastSuccess ?? 0)
    const remaining = DESKTOP_UPDATE_INTERVAL_MS - elapsed
    return this.lastSuccess !== null && remaining > 0 ? remaining : DESKTOP_UPDATE_INTERVAL_MS
  }

  private check(manual: boolean): Promise<void> {
    if (this.disposed || this.installing()) return Promise.resolve()
    if (!manual && this.phase.kind === "error" && this.phase.version) return Promise.resolve()
    if (this.active && (!manual || this.active.manual)) return Promise.resolve()
    const now = this.client.now?.() ?? Date.now()
    if (!manual && this.lastSuccess !== null && now - this.lastSuccess < DESKTOP_UPDATE_INTERVAL_MS) return Promise.resolve()

    const id = ++this.sequence
    this.active = { manual }
    if (manual) this.paint({ kind: "checking" })
    return this.client.check(manual).then(
      (result) => {
        if (this.disposed || id !== this.sequence) return
        this.active = null
        const version = result.status === "available" ? result.offer?.version : undefined
        if (version || result.status === "current" || result.status === "unsupported") {
          this.lastSuccess = this.client.now?.() ?? Date.now()
        } else this.lastSuccess = null
        if (version) {
          this.paint(!manual && this.client.dismissedVersion() === version
            ? { kind: "hidden" }
            : { kind: "available", version })
        } else if (result.status === "current") {
          this.paint({ kind: manual ? "current" : "hidden" })
        } else if (result.status === "unsupported") {
          if (manual) this.paint({ kind: "unsupported", message: result.message ?? "" })
          else if (this.phase.kind === "error") this.paint({ kind: "hidden" })
        } else if (manual) {
          this.paint({ kind: "error", message: result.message ?? "" })
        }
      },
      (error: unknown) => {
        if (this.disposed || id !== this.sequence) return
        this.active = null
        this.lastSuccess = null
        if (manual) this.paint({ kind: "error", message: errorMessage(error) })
      },
    )
  }

  install() {
    if (this.disposed || this.phase.kind !== "available") return
    this.installVersion(this.phase.version)
  }

  retry() {
    if (this.disposed || this.phase.kind !== "error") return
    if (this.phase.version) this.installVersion(this.phase.version)
    else void this.checkManually()
  }

  private installVersion(version: string) {
    // Invalidate any background request before painting Working. A response
    // from that request must not bring the install button back during download.
    const id = ++this.sequence
    this.active = null
    this.paint({ kind: "working" })
    void this.client.install(version).then(
      () => {
        if (!this.disposed && id === this.sequence) {
          this.stopProgress()
          this.paint({ kind: "restarting" })
        }
      },
      (error: unknown) => {
        if (!this.disposed && id === this.sequence) {
          this.stopProgress()
          this.paint({ kind: "error", message: errorMessage(error), version })
        }
      },
    )
    void this.pollProgress(id, version)
  }

  private async pollProgress(id: number, version: string) {
    const request = new AbortController()
    this.progressRequest = request
    const timeout = setTimeout(() => request.abort(), 5000)
    try {
      const progress = await this.client.progress(request.signal)
      if (!this.disposed && id === this.sequence && this.phase.kind === "working" &&
        progress.version === version && progress.stage !== "idle" && progress.stage !== "error") {
        this.paint({ kind: "working", progress })
      }
    } catch {
      // The install request owns the outcome. A failed observation is not a
      // failed installation and must not enable a competing retry.
    } finally {
      clearTimeout(timeout)
      if (this.progressRequest === request) this.progressRequest = undefined
      if (!this.disposed && id === this.sequence && this.phase.kind === "working") {
        this.progressTimer = setTimeout(() => { void this.pollProgress(id, version) }, 500)
      }
    }
  }

  private stopProgress() {
    clearTimeout(this.progressTimer)
    this.progressTimer = undefined
    this.progressRequest?.abort()
    this.progressRequest = undefined
  }

  dismiss() {
    if (this.disposed || this.phase.kind !== "available") return
    this.client.dismissVersion(this.phase.version)
    this.paint({ kind: "hidden" })
  }

  close() {
    if (this.disposed || this.installing() || this.phase.kind === "checking") return
    if (this.phase.kind === "available") this.dismiss()
    else this.paint({ kind: "hidden" })
  }

  dispose() {
    this.disposed = true
    this.sequence++
    this.active = null
    this.stopProgress()
  }

  private installing() {
    return this.phase.kind === "working" || this.phase.kind === "restarting"
  }

  private paint(phase: DesktopUpdatePhase) {
    this.phase = phase
    this.client.onChange(phase)
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

export function watchDesktopUpdates(controller: DesktopUpdateController): () => void {
  let stopped = false
  let timer: number
  const check = () => {
    void controller.autoCheck().finally(() => {
      if (stopped) return
      window.clearTimeout(timer)
      timer = window.setTimeout(check, controller.nextCheckDelay)
    })
  }
  const visible = () => { if (document.visibilityState === "visible") check() }
  window.addEventListener("focus", check)
  window.addEventListener("online", check)
  document.addEventListener("visibilitychange", visible)
  check()
  return () => {
    stopped = true
    window.clearTimeout(timer)
    window.removeEventListener("focus", check)
    window.removeEventListener("online", check)
    document.removeEventListener("visibilitychange", visible)
    controller.dispose()
  }
}
