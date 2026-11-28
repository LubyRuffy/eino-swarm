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
const GRAPH_RETRY_MS = 500

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
let readController: AbortController | undefined
let wantedOwner: string | undefined
let wantedRevision = 0
let previewPending = false
let retryRevision: number | undefined
let retryTimer: number | undefined

/** One inspector visit owns its reads and invalidations. A revision frame says
 *  "something changed"; the picture always comes from a read, so a lost or
 *  duplicated frame costs nothing. */
export const useGraph = create<GraphState>((set, get) => ({
  loading: false,
  connected: false,
  connect: (owner) => {
    const token = ++visit
    closeStream?.()
    readController?.abort()
    reading = undefined
    wantedOwner = undefined
    wantedRevision = 0
    previewPending = false
    retryRevision = undefined
    window.clearTimeout(retryTimer)
    set({ owner, view: undefined, loading: true, connected: false, error: undefined })
    if (owner.threadId) {
      closeStream = subscribeGraphEvents(owner.threadId, (event) => {
        if (token !== visit) return
        if (event.kind === "connection") {
          set({ connected: event.connected })
          if (event.connected) void get().refresh()
          return
        }
        const current = get().view
        const switched = event.ownerId !== undefined && event.ownerId !== (wantedOwner ?? current?.owner_id)
        if (event.ownerId !== undefined) wantedOwner = event.ownerId
        if (switched) {
          // Revisions from two owners are independent. Drop the previous
          // picture and any read started before this announcement.
          readController?.abort()
          reading = undefined
          wantedRevision = event.revision
          retryRevision = undefined
          if (current?.owner_id !== wantedOwner) set({ view: undefined })
        } else if (event.kind === "preview") {
          previewPending = true
        } else {
          if (event.revision <= (current?.revision ?? -1) || event.revision <= wantedRevision) return
          wantedRevision = event.revision
        }
        void get().refresh()
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
      readController?.abort()
      readController = undefined
      reading = undefined
      window.clearTimeout(retryTimer)
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
    const controller = new AbortController()
    readController = controller
    const isCurrent = () => token === visit && !controller.signal.aborted
    const requestedRevision = wantedRevision
    previewPending = false
    window.clearTimeout(retryTimer)
    set({ loading: !get().view, error: undefined })
    reading = (async () => {
      try {
        const view = owner.threadId
          ? await graphAPI.thread(owner.threadId, controller.signal)
          : await graphAPI.project(owner.projectId!, controller.signal)
        if (!isCurrent() || (wantedOwner && view.owner_id !== wantedOwner)) return
        const current = get().view
        // An older read that lost a race must not roll the picture back.
        if (current && view.owner_id === current.owner_id && view.revision < current.revision) return
        set({ view, error: undefined })
      } catch (error) {
        if (isCurrent()) set({ error: error instanceof Error ? error.message : String(error) })
      } finally {
        if (isCurrent()) {
          readController = undefined
          reading = undefined
          set({ loading: false })
          const current = get().view
          if (previewPending || wantedRevision > requestedRevision) {
            await get().refresh()
          } else if (wantedRevision > (current?.revision ?? (wantedOwner ? -1 : 0)) && retryRevision !== wantedRevision) {
            // One delayed retry per announced revision covers an interrupted
            // or stale snapshot without looping during an outage.
            retryRevision = wantedRevision
            retryTimer = window.setTimeout(() => { if (token === visit) void get().refresh() }, GRAPH_RETRY_MS)
          }
        }
      }
    })()
    return reading
  },
}))
