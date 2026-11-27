import { create } from "zustand"
import { graphAPI } from "@/lib/graph/api"
import { subscribeGraphEvents } from "@/lib/graph/events"
import type { GraphView } from "@/lib/graph/types"

export interface GraphOwner {
  threadId?: string
  projectId?: string
}

/** A project page has no conversation connection to ride, so it re-reads on a timer. */
export const PROJECT_POLL_MS = 4000

interface GraphState {
  owner?: GraphOwner
  view?: GraphView
  loading: boolean
  connected: boolean
  error?: string
  connect: (owner: GraphOwner) => () => void
  refresh: () => Promise<void>
}

let visit = 0
let closeStream: (() => void) | undefined
let reading: Promise<void> | undefined
let wantedRevision = 0

/** One inspector visit owns its reads and invalidations. A revision frame says
 *  "something changed"; the picture always comes from a read, so a lost or
 *  duplicated frame costs nothing. */
export const useGraph = create<GraphState>((set, get) => ({
  loading: false,
  connected: false,
  connect: (owner) => {
    const token = ++visit
    closeStream?.()
    reading = undefined
    wantedRevision = 0
    set({ owner, view: undefined, loading: true, connected: false, error: undefined })
    const invalidate = (revision: number) => {
      const current = get().view
      if (revision <= (current?.revision ?? -1) || revision <= wantedRevision) return
      wantedRevision = revision
      void get().refresh()
    }
    if (owner.threadId) {
      closeStream = subscribeGraphEvents(owner.threadId, (event) => {
        if (token !== visit) return
        if (event.kind === "revision") invalidate(event.revision)
        else {
          set({ connected: event.connected })
          if (event.connected) void get().refresh()
        }
      })
    } else {
      const timer = window.setInterval(() => { if (token === visit) void get().refresh() }, PROJECT_POLL_MS)
      closeStream = () => window.clearInterval(timer)
      set({ connected: true })
    }
    void get().refresh()
    return () => {
      if (token !== visit) return
      ++visit
      reading = undefined
      closeStream?.()
      closeStream = undefined
      set({ owner: undefined, view: undefined, connected: false, loading: false })
    }
  },
  refresh: async () => {
    const owner = get().owner
    if (!owner) return
    if (reading) return reading
    const token = visit
    set({ loading: !get().view, error: undefined })
    reading = (async () => {
      try {
        const view = owner.threadId
          ? await graphAPI.thread(owner.threadId)
          : await graphAPI.project(owner.projectId!)
        if (token !== visit) return
        const current = get().view
        // An older read that lost a race must not roll the picture back.
        if (current && view.owner_id === current.owner_id && view.revision < current.revision) return
        set({ view, error: undefined })
      } catch (error) {
        if (token === visit) set({ error: error instanceof Error ? error.message : String(error) })
      } finally {
        if (token === visit) {
          reading = undefined
          set({ loading: false })
          const current = get().view
          if (wantedRevision > (current?.revision ?? 0)) await get().refresh()
        }
      }
    })()
    return reading
  },
}))
