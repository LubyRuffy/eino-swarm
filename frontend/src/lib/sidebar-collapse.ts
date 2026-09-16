/** Which project folders the user opened or closed. Missing keys follow
 *  the open conversation (or the selected empty project), so a first visit
 *  is not a wall of collapsed names. */

const KEY = "zwai.sidebar.project-expanded"

export function isProjectExpanded(
  projectId: string,
  opts: {
    activeProjectId?: string
    selectedId?: string
    overrides: Record<string, boolean>
  },
): boolean {
  if (Object.prototype.hasOwnProperty.call(opts.overrides, projectId)) {
    return opts.overrides[projectId]
  }
  return projectId === opts.activeProjectId || projectId === opts.selectedId
}

export function readProjectExpanded(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw == null) return {}
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return {}
    const out: Record<string, boolean> = {}
    for (const [id, value] of Object.entries(parsed as Record<string, unknown>)) {
      if (typeof value === "boolean") out[id] = value
    }
    return out
  } catch {
    return {}
  }
}

export function writeProjectExpanded(map: Record<string, boolean>): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(map))
  } catch {
    // A preference is not worth failing to start over.
  }
}
