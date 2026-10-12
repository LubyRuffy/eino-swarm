import type { DesktopUpdate } from "./api"

// Match the existing release cache so a long-lived window notices new builds
// without repeatedly reaching GitHub when the user switches applications.
export const DESKTOP_UPDATE_INTERVAL_MS = 6 * 60 * 60 * 1000

export type DesktopUpdatePhase =
  | { kind: "hidden" }
  | { kind: "checking" }
  | { kind: "current" }
  | { kind: "available"; version: string }
  | { kind: "working" }
  | { kind: "restarting" }
  | { kind: "unsupported"; message: string }
  | { kind: "error"; message: string }

interface UpdateClient {
  check: (fresh: boolean) => Promise<DesktopUpdate>
  install: (version: string) => Promise<{ status: string }>
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
    const version = this.phase.version
    // Invalidate any background request before painting Working. A response
    // from that request must not bring the install button back during download.
    const id = ++this.sequence
    this.active = null
    this.paint({ kind: "working" })
    void this.client.install(version).then(
      () => {
        if (!this.disposed && id === this.sequence) this.paint({ kind: "restarting" })
      },
      (error: unknown) => {
        if (!this.disposed && id === this.sequence) this.paint({ kind: "error", message: errorMessage(error) })
      },
    )
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
