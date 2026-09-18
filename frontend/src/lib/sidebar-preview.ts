import type { Thread } from "@/lib/types"

/** How many conversations a project folder or Recents shows before
 *  Show more. A long-lived project otherwise paints a novel. */
export const SIDEBAR_PREVIEW_LIMIT = 5

/** Conversations idle longer than this sit behind Show more even when
 *  the folder has fewer than SIDEBAR_PREVIEW_LIMIT rows. */
export const SIDEBAR_PREVIEW_MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000

/** Split one sidebar group into the rows shown by default and the ones
 *  behind Show more. A row is previewed when it is among the first
 *  SIDEBAR_PREVIEW_LIMIT that were active inside SIDEBAR_PREVIEW_MAX_AGE_MS.
 *  keepIds (the open conversation, a running one) stay in the preview so
 *  the row you are looking at cannot vanish under More. */
export function splitSidebarPreview(
  threads: Thread[],
  opts?: { now?: Date; keepIds?: Iterable<string> },
): { visible: Thread[]; hidden: Thread[] } {
  const now = opts?.now ?? new Date()
  const keep = new Set(opts?.keepIds)
  const cutoff = now.getTime() - SIDEBAR_PREVIEW_MAX_AGE_MS
  let freshShown = 0
  const visible: Thread[] = []
  const hidden: Thread[] = []
  for (const thread of threads) {
    const ts = Date.parse(thread.last_active_at)
    const fresh = Number.isNaN(ts) || ts >= cutoff
    const keepThis = keep.has(thread.id)
    if (keepThis || (fresh && freshShown < SIDEBAR_PREVIEW_LIMIT)) {
      visible.push(thread)
      if (fresh) freshShown++
      continue
    }
    hidden.push(thread)
  }
  return { visible, hidden }
}
