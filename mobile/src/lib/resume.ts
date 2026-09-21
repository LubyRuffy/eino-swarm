import type { ProjectView, RunningView, ThreadView, ThreadDetail } from "./rpc"

// Live rows come from the running roster *and* the listing flag. A host
// that only sets thread.running still has to look in-progress on the phone.
// A parked wait is live too: the next turn is that check, not a dead inbox.
export function collectLive(running: RunningView[], threads: ThreadView[]): RunningView[] {
  const out: RunningView[] = []
  const seen = new Set<string>()
  const push = (row: RunningView) => {
    if (!row.thread_id || seen.has(row.thread_id)) return
    seen.add(row.thread_id)
    out.push(row)
  }
  for (const r of running) push(r)
  for (const th of threads) {
    if (!th.running && !th.waiting) continue
    push({
      thread_id: th.id,
      title: th.title,
      waiting: th.waiting,
    })
  }
  return out
}

// Jump into a live turn after bind, else the thread this phone last
// opened. An idle first bind stays on the inbox so Start is still there;
// Back is not a trap (resumedRef). Last may be older than the slim list —
// open still keys off the id; a missing thread clears it.
export function pickResumeThread(
  lastId: string,
  running: RunningView[],
  threads: ThreadView[],
): string {
  const live = collectLive(running, threads)
  if (lastId && live.some((r) => r.thread_id === lastId)) return lastId
  if (live[0]) return live[0].thread_id
  return lastId
}

// Inbox poll used to setState a new array every 2s even when nothing
// changed, which replaces the row mid-tap and eats the click. Fingerprint
// the tappable bits so a quiet roster does not remount.
export function rosterFingerprint(
  projects: ProjectView[],
  threads: ThreadView[],
  running: RunningView[],
  more: boolean,
  cursor: string,
): string {
  const ps = projects.map((p) => `${p.id}\0${p.name}`).join("|")
  const live = running
    .map((r) =>
      [r.thread_id, r.title, r.action ?? "", r.waiting ? "1" : "", r.turn_id ?? "", r.ask_user ? "1" : ""].join("\0"),
    )
    .join("|")
  const th = threads
    .map((t) =>
      [
        t.id,
        t.title,
        t.summary ?? "",
        t.waiting ? "1" : "",
        t.running ? "1" : "",
        t.project_id ?? "",
        t.last_active_at,
      ].join("\0"),
    )
    .join("|")
  return [ps, live, th, more ? "1" : "0", cursor].join("#")
}

export function detailFromListing(
  id: string,
  running: RunningView[],
  threads: ThreadView[],
): ThreadDetail {
  const live = running.find((r) => r.thread_id === id)
  const th = threads.find((t) => t.id === id)
  const title = (th?.title || live?.title || "").trim() || id
  const waiting = Boolean(live?.waiting || th?.waiting)
  const liveTurn = Boolean(live && (live.turn_id || live.ask_user || live.action))
  return {
    id,
    title,
    ...(waiting ? { waiting: true } : {}),
    ...(liveTurn
      ? {
          running: {
            thread_id: id,
            title: live?.title || title,
            ...(live?.turn_id ? { turn_id: live.turn_id } : {}),
            ...(live?.action ? { action: live.action } : {}),
            ...(live?.ask_user ? { ask_user: true } : {}),
            ...(live?.waiting ? { waiting: true } : {}),
          },
        }
      : {}),
  }
}
