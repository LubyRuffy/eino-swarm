import type { RunningView, ThreadView } from "./rpc"

// Live rows come from the running roster *and* the listing flag. A host
// that only sets thread.running still has to look in-progress on the phone.
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
    if (!th.running) continue
    push({ thread_id: th.id, title: th.title })
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
