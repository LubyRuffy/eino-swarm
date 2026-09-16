/** Which project folders the user opened or closed. Missing keys follow
 *  the open conversation (or the selected empty project), so a first visit
 *  is not a wall of collapsed names. */

const FOLDER_KEY = "zwai.sidebar.project-expanded"
const SECTION_KEY = "zwai.sidebar.section-expanded"

export const SECTION_IDS = ["pinned", "projects", "recents"] as const
export type SectionId = (typeof SECTION_IDS)[number]
export type SectionExpanded = Record<SectionId, boolean>

export function defaultSectionExpanded(): SectionExpanded {
  return { pinned: true, projects: true, recents: true }
}

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
    const raw = localStorage.getItem(FOLDER_KEY)
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
    localStorage.setItem(FOLDER_KEY, JSON.stringify(map))
  } catch {
    // A preference is not worth failing to start over.
  }
}

/** Pinned / Projects / Recents fold independently of the folders inside
 *  them. Missing or garbage storage is open, matching a first visit. */
export function readSectionExpanded(): SectionExpanded {
  const out = defaultSectionExpanded()
  try {
    const raw = localStorage.getItem(SECTION_KEY)
    if (raw == null) return out
    const parsed: unknown = JSON.parse(raw)
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return out
    const src = parsed as Record<string, unknown>
    for (const id of SECTION_IDS) {
      if (typeof src[id] === "boolean") out[id] = src[id]
    }
    return out
  } catch {
    return defaultSectionExpanded()
  }
}

export function writeSectionExpanded(map: SectionExpanded): void {
  try {
    localStorage.setItem(SECTION_KEY, JSON.stringify(map))
  } catch {
    // Same as folders: a preference is not worth failing to start over.
  }
}
