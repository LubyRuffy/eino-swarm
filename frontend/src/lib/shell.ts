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

/** Reserve a presence id and hold the connection. A client that does not
 *  implement both calls is left alone: unit tests stub the API without a
 *  socket, and a missing hold must not fail boot. */
export function startPresence(
  client: {
    presence?: (surface: "desktop" | "web") => Promise<{ id: string }>
    holdPresence?: (id: string) => void
  },
  surface: "desktop" | "web",
): void {
  if (typeof client.presence !== "function" || typeof client.holdPresence !== "function") {
    return
  }
  const hold = client.holdPresence
  void client
    .presence(surface)
    .then((reserved) => {
      if (reserved?.id) hold(reserved.id)
    })
    .catch(() => {
      // The listing still works if the hold fails. The next tick can retry
      // only by reloading; a toast here would fire on every background tab.
    })
}
