import type { Project, ScheduleCreate, Thread } from "@/lib/types"

/** Destination for a human-created wait. Not a third kind: standalone
 *  mints a conversation per fire; thread wakes one that already exists. */
export const RUNS_IN_NEW = "new"

const THREAD_PREFIX = "thread:"

export function runsInThreadValue(id: string): string {
  return `${THREAD_PREFIX}${id}`
}

export function runsInThreadId(value: string): string | undefined {
  if (!value.startsWith(THREAD_PREFIX)) return undefined
  const id = value.slice(THREAD_PREFIX.length).trim()
  return id || undefined
}

export function runsInFromSchedule(row: { kind?: string; thread_id?: string }): string {
  const id = (row.thread_id ?? "").trim()
  if ((row.kind ?? "").trim() === "thread" && id) return runsInThreadValue(id)
  return RUNS_IN_NEW
}

export function applyRunsIn(
  body: ScheduleCreate,
  value: string,
  projectId: string,
): ScheduleCreate {
  const threadId = runsInThreadId(value)
  if (threadId) {
    return { ...body, kind: "thread", thread_id: threadId }
  }
  const next: ScheduleCreate = { ...body, kind: "standalone" }
  if (projectId !== "none" && projectId.trim()) next.project_id = projectId
  return next
}

export function livePickerThreads(threads: Thread[]): Thread[] {
  return threads.filter((row) => !row.archived)
}

export function pinnedPickerThreads(threads: Thread[]): Thread[] {
  return livePickerThreads(threads)
    .filter((row) => row.pinned)
    .sort((a, b) => {
      const tb = Date.parse(b.pinned_at ?? "")
      const ta = Date.parse(a.pinned_at ?? "")
      if (!Number.isNaN(tb) && !Number.isNaN(ta) && tb !== ta) return tb - ta
      return b.id.localeCompare(a.id)
    })
}

export function pickerThreadLabel(row: Pick<Thread, "title">, untitled: string): string {
  const title = (row.title ?? "").trim()
  return title || untitled
}

/** Unpinned live conversations, grouped by project. Missing projects fold
 *  into the no-project bucket so a stale id never becomes a heading. */
export function unpinnedPickerGroups(
  threads: Thread[],
  projects: Project[],
): Array<{ key: string; label: string; threads: Thread[] }> {
  const pinned = new Set(pinnedPickerThreads(threads).map((row) => row.id))
  const rest = livePickerThreads(threads)
    .filter((row) => !pinned.has(row.id))
    .sort((a, b) => {
      const tb = Date.parse(b.last_active_at ?? "")
      const ta = Date.parse(a.last_active_at ?? "")
      if (!Number.isNaN(tb) && !Number.isNaN(ta) && tb !== ta) return tb - ta
      return b.id.localeCompare(a.id)
    })
  const names = new Map(projects.map((p) => [p.id, p.name]))
  const none: Thread[] = []
  const by = new Map<string, Thread[]>()
  for (const row of rest) {
    const pid = (row.project_id ?? "").trim()
    if (!pid || !names.has(pid)) {
      none.push(row)
      continue
    }
    const list = by.get(pid) ?? []
    list.push(row)
    by.set(pid, list)
  }
  const groups: Array<{ key: string; label: string; threads: Thread[] }> = []
  if (none.length > 0) groups.push({ key: "", label: "", threads: none })
  for (const p of projects) {
    const list = by.get(p.id)
    if (list && list.length > 0) {
      groups.push({ key: p.id, label: p.name, threads: list })
    }
  }
  return groups
}
