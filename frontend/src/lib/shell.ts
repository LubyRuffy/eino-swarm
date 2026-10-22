/** Which window this page is. The engine mode is always "engine", because
 *  one process serves every shell. Traffic lights follow the window, not
 *  that mode. */

export function desktopShellFrom(search: string, hasWails: boolean): boolean {
  const raw = search.startsWith("?") ? search.slice(1) : search
  if (new URLSearchParams(raw).get("shell") === "desktop") return true
  return hasWails
}

export function desktopShell(): boolean {
  if (typeof window === "undefined") return false
  const w = window as Window & { _wails?: unknown }
  return desktopShellFrom(window.location.search, w._wails != null)
}

/** The native launcher publishes its window PID so an update can close that
 *  window after starting the replacement. A browser has no native PID. */
export function desktopProcessID(search: string): number | undefined {
  const raw = new URLSearchParams(search).get("pid") ?? ""
  const pid = Number(raw)
  return /^\d+$/.test(raw) && Number.isInteger(pid) && pid > 0 && pid <= 2147483647 ? pid : undefined
}

/** A loopback engine advertises open_url because the desktop window has no
 *  other way out of the webview. A browser tab on that same process opens
 *  a new window itself; calling the endpoint would spawn a browser on the host. */
export function openLinksNatively(capability: boolean | undefined, shell: boolean): boolean {
  return Boolean(capability) && shell
}

export function uniqueSurfaces(clients: { surface?: string }[] | undefined): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const client of clients ?? []) {
    const surface = (client.surface ?? "").trim()
    if (!surface || seen.has(surface)) continue
    seen.add(surface)
    out.push(surface)
  }
  return out
}

/** Native presence follows the window process, including webview reloads.
 *  Leave the launcher's hold alive; other clients reserve their own id.
 *  A missing hold must not fail boot. */
export function startPresence(
  client: {
    presence?: (surface: "desktop" | "web") => Promise<{ id: string }>
    holdPresence?: (id: string) => void
  },
  surface: "desktop" | "web",
  search = typeof window === "undefined" ? "" : window.location.search,
): void {
  const id = surface === "desktop" ? new URLSearchParams(search).get("presence") : null
  if (id && /^pc_[0-9a-f]{16}$/.test(id)) return
  if (typeof client.presence !== "function" || typeof client.holdPresence !== "function") {
    return
  }
  const hold = client.holdPresence
  void client.presence(surface)
    .then((reserved) => {
      if (reserved?.id) hold(reserved.id)
    })
    .catch(() => {
      // The listing still works if the hold fails. The next tick can retry
      // only by reloading; a toast here would fire on every background tab.
    })
}
