import { mergeThreadList, threadListOverlay } from "@/lib/thread-title"
import type { Thread, ThreadStatus } from "@/lib/types"

export function applyThreadListing(
  threads: Thread[], incoming: Thread[], activeId: string | undefined,
  status: ThreadStatus, baseline: Thread[],
): Thread[] {
  return mergeThreadList(threads, incoming, threadListOverlay(activeId, status), baseline)
}
