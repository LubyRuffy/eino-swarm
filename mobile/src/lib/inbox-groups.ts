import { t } from "@/lib/i18n"
import type { ProjectView, RunningView, ThreadView } from "@/lib/rpc"

export type InboxGroup = {
  id: string
  name: string
  threads: ThreadView[]
}

/** Projects keep every conversation that belongs to them, including one
 *  the host left off `threads` because it is already In progress. Recents
 *  stays the idle leftover: a live row with no project is already on the
 *  roster and must not be painted twice. */
export function groupInbox(
  projects: ProjectView[],
  threads: ThreadView[],
  live: RunningView[],
): InboxGroup[] {
  const names = new Map(projects.map((p) => [p.id, p.name]))
  const by = new Map<string, ThreadView[]>()
  const placed = new Set<string>()
  const rest: ThreadView[] = []
  const liveIDs = new Set(live.map((r) => r.thread_id))

  for (const th of threads) {
    if (th.project_id && names.has(th.project_id)) {
      const list = by.get(th.project_id) ?? []
      list.push(th)
      by.set(th.project_id, list)
      placed.add(th.id)
      continue
    }
    if (liveIDs.has(th.id)) continue
    rest.push(th)
  }

  const extras = new Map<string, ThreadView[]>()
  for (const r of live) {
    if (!r.thread_id || !r.project_id || !names.has(r.project_id) || placed.has(r.thread_id)) {
      continue
    }
    const list = extras.get(r.project_id) ?? []
    list.push(threadFromLive(r))
    extras.set(r.project_id, list)
    placed.add(r.thread_id)
  }

  const out: InboxGroup[] = []
  for (const p of projects) {
    const head = extras.get(p.id) ?? []
    const tail = by.get(p.id) ?? []
    out.push({ id: p.id, name: p.name, threads: head.concat(tail) })
  }
  if (rest.length) out.push({ id: "", name: t("home.recent"), threads: rest })
  return out
}

function threadFromLive(r: RunningView): ThreadView {
  return {
    id: r.thread_id,
    title: r.title || r.thread_id,
    project_id: r.project_id,
    running: !r.waiting,
    waiting: Boolean(r.waiting),
    last_active_at: r.last_active_at ?? "",
    summary: r.action,
  }
}
