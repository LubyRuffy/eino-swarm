/** Conversation-list width. The title bar's leading cluster tracks this
 *  through a CSS variable so dragging the border does not re-render
 *  the transcript on every pointer frame. */

export const SIDEBAR_WIDTH_DEFAULT = 256
export const SIDEBAR_WIDTH_MIN = 200
export const SIDEBAR_WIDTH_MAX = 480
export const SIDEBAR_WIDTH_VAR = "--zwai-sidebar-width"

const SIDEBAR_WIDTH_KEY = "zwai.sidebar.width"

export function clampSidebarWidth(px: number): number {
  return Math.min(SIDEBAR_WIDTH_MAX, Math.max(SIDEBAR_WIDTH_MIN, Math.round(px)))
}

/** Missing or unreadable storage means the original 16rem column. */
export function readSidebarWidth(): number {
  try {
    const raw = localStorage.getItem(SIDEBAR_WIDTH_KEY)
    if (raw == null) return SIDEBAR_WIDTH_DEFAULT
    const n = Number(raw)
    if (!Number.isFinite(n) || n <= 0) return SIDEBAR_WIDTH_DEFAULT
    return clampSidebarWidth(n)
  } catch {
    return SIDEBAR_WIDTH_DEFAULT
  }
}

/** Paint the variable before the first frame so the title bar and the
 *  list agree even when a remembered width is not the default. */
export function hydrateSidebarWidth(): number {
  const width = readSidebarWidth()
  paintSidebarWidth(width)
  return width
}

/** Live preview during a drag. Writing storage here used to hitch every
 *  pointer frame against a long conversation list. */
export function paintSidebarWidth(px: number): number {
  const width = clampSidebarWidth(px)
  if (typeof document !== "undefined") {
    document.documentElement.style.setProperty(SIDEBAR_WIDTH_VAR, `${width}px`)
  }
  return width
}

export function applySidebarWidth(px: number): number {
  const width = paintSidebarWidth(px)
  try {
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(width))
  } catch {
    // A preference is not worth failing to start over.
  }
  return width
}
