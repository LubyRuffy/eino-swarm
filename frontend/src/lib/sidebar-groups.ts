import type { Thread } from "@/lib/types"
import { t, type Locale } from "@/lib/i18n"
import { relativeDay } from "@/lib/utils"

const DAY_ORDER = ["time.today", "time.yesterday", "time.week", "time.month", "time.earlier"] as const

/** Day buckets for the default recency list. A dragged rank flattens the
 *  list: date headers would lie about a row you just pulled out of Yesterday. */
export function threadSections(
  threads: Thread[],
  locale: Locale = "en",
): { label?: string; threads: Thread[] }[] {
  if (threads.some((t) => (t.sort_rank ?? 0) !== 0)) {
    return [{ threads }]
  }
  const buckets = new Map<string, Thread[]>()
  for (const t of threads) {
    const label = relativeDay(t.last_active_at, new Date(), locale)
    const list = buckets.get(label) ?? []
    list.push(t)
    buckets.set(label, list)
  }
  return DAY_ORDER.map((key) => t(locale, key))
    .filter((label) => buckets.has(label))
    .map((label) => ({
      label,
      threads: buckets.get(label)!,
    }))
}

export type SidebarBuckets = {
  pinned: Thread[]
  recents: Thread[]
  byProject: Record<string, Thread[]>
}

/** Split the sidebar the way Codex does: pinned topics to watch, project
 *  folders, and Recents for conversations that belong to no project.
 *
 *  Each group sorts on its own `sort_rank`. Recents and a project can both
 *  have a row ranked 1000 after separate drags; a global rank would interleave
 *  them and the folders would lie. */
export function sidebarBuckets(threads: Thread[]): SidebarBuckets {
  const pinned = orderPinned(threads.filter((th) => th.pinned))
  const recents = orderGroup(threads.filter((th) => !th.project_id))
  const byProject: Record<string, Thread[]> = {}
  for (const th of threads) {
    if (!th.project_id) continue
    const list = byProject[th.project_id] ?? []
    list.push(th)
    byProject[th.project_id] = list
  }
  for (const id of Object.keys(byProject)) {
    byProject[id] = orderGroup(byProject[id])
  }
  return { pinned, recents, byProject }
}

function orderPinned(threads: Thread[]): Thread[] {
  return [...threads].sort((a, b) => {
    const ta = a.pinned_at || a.last_active_at
    const tb = b.pinned_at || b.last_active_at
    return tb.localeCompare(ta)
  })
}

function orderGroup(threads: Thread[]): Thread[] {
  // Rank 0 is "never dragged", not "always first". Merge unranked by
  // last activity through the ranked subsequence so a stale global
  // cannot sit above a project topic that just ran.
  const unpinned: Thread[] = []
  const pinned: Thread[] = []
  for (const th of threads) {
    if ((th.sort_rank ?? 0) === 0) unpinned.push(th)
    else pinned.push(th)
  }
  unpinned.sort((a, b) => b.last_active_at.localeCompare(a.last_active_at))
  pinned.sort((a, b) => (a.sort_rank ?? 0) - (b.sort_rank ?? 0))
  if (unpinned.length === 0) return pinned
  if (pinned.length === 0) return unpinned
  const out: Thread[] = []
  let i = 0
  let j = 0
  while (i < unpinned.length && j < pinned.length) {
    if (unpinned[i].last_active_at.localeCompare(pinned[j].last_active_at) >= 0) {
      out.push(unpinned[i++])
    } else {
      out.push(pinned[j++])
    }
  }
  return out.concat(unpinned.slice(i), pinned.slice(j))
}
