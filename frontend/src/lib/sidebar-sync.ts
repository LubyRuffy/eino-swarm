/** How often the sidebar re-reads GET /api/threads so a conversation that
 *  started (or finished) in the background lights up without a click.
 *  Per-row polling is what `running` on the listing already avoided. */
export const THREAD_LIST_SYNC_MS = 2000

/** Keep the folder column honest while this window is open. Hidden tabs
 *  skip the tick; becoming visible again is an immediate catch-up. */
export function startSidebarSync(refresh: () => void | Promise<void>): () => void {
  const tick = () => {
    if (document.hidden) return
    void refresh()
  }
  const id = window.setInterval(tick, THREAD_LIST_SYNC_MS)
  const onVis = () => {
    if (!document.hidden) void refresh()
  }
  document.addEventListener("visibilitychange", onVis)
  return () => {
    window.clearInterval(id)
    document.removeEventListener("visibilitychange", onVis)
  }
}
