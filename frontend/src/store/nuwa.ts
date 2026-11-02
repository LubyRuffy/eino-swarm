import { create } from "zustand"
import { nuwaAPI, subscribeNuwa } from "@/lib/nuwa-api"
import type { NuwaSnapshot } from "@/lib/nuwa"

interface NuwaState {
  projectId?: string
  snapshot?: NuwaSnapshot
  loading: boolean
  connected: boolean
  error?: string
  connect: (projectId: string) => () => void
  refresh: () => Promise<void>
}

let visit = 0
let closeStream: (() => void) | undefined
let reading = false
let wantedRevision = -1

/** One mounted project view owns its stream. The visit identity also rejects
 * old A → B → A responses, while revisions reject older snapshots in a visit. */
export const useNuwa = create<NuwaState>((set, get) => ({
  loading: false,
  connected: false,
  connect: (projectId) => {
    closeStream?.()
    const token = ++visit
    reading = false
    wantedRevision = -1
    set({ projectId, snapshot: undefined, loading: true, connected: false, error: undefined })
    closeStream = subscribeNuwa(projectId, {
      onRevision: (revision) => {
        if (token !== visit || revision <= (get().snapshot?.revision ?? -1)) return
        wantedRevision = Math.max(wantedRevision, revision)
        void get().refresh()
      },
      onOpen: () => {
        if (token !== visit) return
        set({ connected: true })
        void get().refresh()
      },
      onError: () => {
        if (token === visit) set({ connected: false })
      },
    })
    void get().refresh()
    return () => {
      if (token !== visit) return
      ++visit
      reading = false
      closeStream?.()
      closeStream = undefined
      set({ connected: false })
    }
  },
  refresh: async () => {
    const projectId = get().projectId
    if (!projectId || reading) return
    const token = visit
    const requested = wantedRevision
    reading = true
    set({ loading: !get().snapshot, error: undefined })
    try {
      const snapshot = await nuwaAPI.snapshot(projectId)
      if (token !== visit || snapshot.project_id !== projectId) return
      if (snapshot.revision >= (get().snapshot?.revision ?? -1))
        set({ snapshot, loading: false, error: undefined })
    } catch (error) {
      if (token === visit)
        set({ loading: false, error: error instanceof Error ? error.message : String(error) })
    } finally {
      if (token === visit) {
        reading = false
        // An invalidation received during this read needs one fresh read. A
        // stale/erroring server alone never starts a background retry loop.
        if (wantedRevision > requested && wantedRevision > (get().snapshot?.revision ?? -1))
          void get().refresh()
      }
    }
  },
}))
