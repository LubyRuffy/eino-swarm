import { vi } from "vitest"

// The store is tested against a fake API rather than the DOM: what matters
// here is which conversation each action lands in. vi.mock is hoisted, so the
// recorders it reads have to be hoisted with it.
const fake = vi.hoisted(() => ({
  created: 0,
  /** createThread resolves on a later tick, which is the window a fast typist
   *  hits between clicking "New conversation" and pressing Enter. */
  createDelayMs: 0,
  steers: [] as Array<{ id: string; text: string; images?: unknown; files?: string[] }>,
  started: [] as Array<{
    id: string
    text: string
    images?: unknown
    files?: string[]
    fromEventSeq?: number
  }>,
  uploads: [] as string[],
  continues: [] as Array<{ id: string; proceed: boolean }>,
  /** Which project each list call was scoped to. The sidebar no longer
   *  filters; a leftover query would still be a bug. */
  listedProjects: [] as Array<string | undefined>,
  listedThreads: undefined as Array<Record<string, unknown>> | undefined,
  listedFail: false,
  createdIn: [] as Array<string | undefined>,
  reviewed: [] as string[],
  loadedMemory: [] as string[],
  reviewCode: undefined as string | undefined,
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
  discovered: [] as Array<Record<string, unknown>>,
  discoveryWait: undefined as Promise<void> | undefined,
  phoneTimeouts: { idle: 300, first: 30 },
  savedProviders: undefined as unknown,
  savedUI: undefined as
    | {
        locale?: string
        font?: string
        font_size?: string
        content_width?: string
      }
    | undefined,
  listedModels: [] as unknown[],
  threadUsage: undefined as
    | {
        context_tokens: number
        context_window: number
        turn: Record<string, number>
        thread: Record<string, number>
      }
    | undefined,
  enqueued: [] as Array<{ id: string; text: string }>,
  queuedItems: [] as Array<{
    id: string
    thread_id: string
    seq: number
    text: string
    created_at: string
  }>,
  steeredFollowups: [] as Array<{ id: string; fid: string }>,
  requeued: [] as Array<{ id: string; fid: string; text: string }>,
  enqueueIdle: false,
  goals: [] as Array<{ id: string; goal?: string; goal_edit?: boolean; goal_resume?: boolean }>,
  compacts: [] as string[],
  reordered: [] as Array<{ ids: string[]; projectId?: string }>,
  reorderFail: false,
  logEvents: [] as Array<Record<string, unknown>>,
  logHasMore: false,
  logRoster: [] as Array<Record<string, unknown>>,
  logHandler: undefined as
    | ((opts?: { before?: number; limit?: number }) => {
        events: Array<Record<string, unknown>>
        has_more: boolean
        roster?: Array<Record<string, unknown>>
      })
    | undefined,
  subscribeSince: [] as number[],
  answers: [] as Array<Record<string, unknown>>,
  metaBody: undefined as Record<string, unknown> | undefined,
}))

vi.mock("@/lib/api", () => {
  const thread = (id: string) => ({
    id,
    title: "New conversation",
    title_auto: true,
    provider_id: "default",
    archived: false,
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    running: false,
  })
  class ApiError extends Error {
    constructor(
      message: string,
      readonly status: number,
      readonly code?: string,
    ) {
      super(message)
    }
  }
  return {
    ApiError,
    api: {
      meta: async () =>
        fake.metaBody ?? { mode: "web", configured: true, capabilities: {} },
      models: async () => ({
        models: fake.listedModels,
        default: "default",
        mock: true,
      }),
      settings: async () => ({
        models: {
          default: "default",
          providers: [
            {
              id: "default",
              label: "Main",
              base_url: "http://endpoint.invalid/v1",
              model: "alpha",
              catalog: ["alpha"],
              timeout_seconds: 300,
              first_byte_timeout_seconds: 30,
              has_api_key: true,
              ready: true,
              api_key: "must-not-be-written-back",
            },
            {
              id: "blank",
              label: "",
              base_url: "",
              model: "",
              catalog: [],
              timeout_seconds: 300,
              has_api_key: false,
              ready: false,
            },
          ],
        },
      }),
      discoverModels: async (body: Record<string, unknown>) => {
        fake.discovered.push(body)
        await fake.discoveryWait
        return { models: ["alpha", "beta"], context_windows: { alpha: 128000 } }
      },
      saveSettings: async (patch: {
        models?: { providers?: unknown }
        ui?: {
          locale?: string
          font?: string
          font_size?: string
          content_width?: string
        }
      }) => {
        fake.savedProviders = patch.models?.providers
        if (Array.isArray(patch.models?.providers)) {
          const row = patch.models.providers.find((p) => p.id === "default")
          if (typeof row?.timeout_seconds === "number") fake.phoneTimeouts.idle = row.timeout_seconds
          if (typeof row?.first_byte_timeout_seconds === "number") fake.phoneTimeouts.first = row.first_byte_timeout_seconds
        }
        if (patch.ui) fake.savedUI = patch.ui
        return patch
      },
      threads: async (_archived?: boolean, projectId?: string) => {
        fake.listedProjects.push(projectId)
        if (fake.listedFail) throw new Error("offline")
        if (fake.listedThreads) return fake.listedThreads
        return [thread("th_old")]
      },
      projects: async () => [
        {
          id: "pj_1",
          name: "Anchored",
          system_prompt: "",
          workdir: "",
          resolved_workdir: "/tmp/ws",
          memory_enabled: true,
          memory_dir: "/tmp/mem",
          created_at: "",
          updated_at: "",
        },
      ],
      memory: async (id: string) => {
        fake.loadedMemory.push(id)
        return {
          dir: "/tmp/mem",
          enabled: true,
          memory: { text: "", entries: [], chars: 0, limit: 2200, rev: "" },
          skills: [],
        }
      },
      reviewThread: async (id: string) => {
        fake.reviewed.push(id)
        if (fake.reviewCode) {
          throw new ApiError("nothing to review", 409, fake.reviewCode)
        }
        return { turn: { id: "tn_1" } }
      },
      createThread: async (
        _title?: string,
        _providerId?: string,
        projectId?: string,
      ) => {
        fake.created++
        fake.createdIn.push(projectId)
        if (fake.createDelayMs > 0) {
          await new Promise((r) => setTimeout(r, fake.createDelayMs))
        }
        return thread(`th_new_${fake.created}`)
      },
      thread: async (id: string) => ({
        thread: thread(id),
        status: { running: false },
        usage: fake.threadUsage,
      }),
      turns: async () => [],
      threadLog: async (_id: string, opts?: { before?: number; limit?: number }) => {
        if (fake.logHandler) return fake.logHandler(opts)
        return {
          events: fake.logEvents,
          has_more: fake.logHasMore,
          roster: fake.logRoster,
        }
      },
      agentLog: async () => ({ events: [] }),
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      followups: async () => fake.queuedItems,
      schedules: async () => ({ schedules: [], unread: 0 }),
      enqueueFollowup: async (id: string, text: string) => {
        if (fake.enqueueIdle) {
          throw new ApiError("the conversation is not running", 409, "idle")
        }
        const item = {
          id: `fu_${fake.enqueued.length + 1}`,
          thread_id: id,
          seq: fake.enqueued.length + 1,
          text,
          created_at: new Date().toISOString(),
        }
        fake.enqueued.push({ id, text })
        fake.queuedItems.push(item)
        return item
      },
      deleteFollowup: async (_id: string, fid: string) => {
        fake.queuedItems = fake.queuedItems.filter((f) => f.id !== fid)
      },
      requeueFollowup: async (id: string, fid: string, text: string) => {
        fake.requeued.push({ id, fid, text })
        const current = fake.queuedItems.find((f) => f.id === fid)
        if (!current) {
          throw new ApiError("not found", 404)
        }
        const item = { ...current, text, seq: current.seq + 100 }
        fake.queuedItems = [...fake.queuedItems.filter((f) => f.id !== fid), item]
        return item
      },
      steerFollowup: async (id: string, fid: string) => {
        fake.steeredFollowups.push({ id, fid })
        fake.queuedItems = fake.queuedItems.filter((f) => f.id !== fid)
        return { steered: true }
      },
      steer: async (id: string, text: string, images?: unknown, files?: string[]) => {
        const rec: { id: string; text: string; images?: unknown; files?: string[] } = {
          id,
          text,
        }
        if (images) rec.images = images
        if (files?.length) rec.files = files
        fake.steers.push(rec)
        return { steered: false }
      },
      startTurn: async (
        id: string,
        text: string,
        images?: unknown,
        files?: string[],
        fromEventSeq?: number,
      ) => {
        const rec: {
          id: string
          text: string
          images?: unknown
          files?: string[]
          fromEventSeq?: number
        } = { id, text }
        if (images) rec.images = images
        if (files?.length) rec.files = files
        if (fromEventSeq) rec.fromEventSeq = fromEventSeq
        fake.started.push(rec)
        return { id: "tn_new", status: "running" }
      },
      upload: async (id: string) => {
        fake.uploads.push(id)
        return []
      },
      interrupt: async () => ({ interrupted: true }),
      answerTurn: async (id: string, body: Record<string, unknown>) => {
        fake.answers.push({ id, ...body })
        return { answered: true }
      },
      continueTurn: async (id: string, proceed: boolean) => {
        fake.continues.push({ id, proceed })
        return { continued: proceed }
      },
      patchThread: async (
        id: string,
        patch: {
          goal?: string
          title?: string
          goal_edit?: boolean
          goal_resume?: boolean
          pinned?: boolean
        },
      ) => {
        fake.goals.push({
          id,
          goal: patch.goal,
          goal_edit: patch.goal_edit,
          goal_resume: patch.goal_resume,
        })
        return {
          ...thread(id),
          ...patch,
          running: Boolean(patch.goal_resume),
          goal_blocked: patch.goal_resume ? false : undefined,
        }
      },
      compactThread: async (id: string) => {
        fake.compacts.push(id)
        return {
          thread: { ...thread(id), compacted: true },
          status: { running: false },
        }
      },
      reorderThreads: async (ids: string[], projectId?: string) => {
        fake.reordered.push({ ids, projectId })
        if (fake.reorderFail) throw new Error("could not pin the order")
        return ids.map((id) => ({ ...thread(id), sort_rank: 1000 }))
      },
    },
  }
})

vi.mock("@/lib/stream", () => ({
  subscribeEvents: (
    _id: string,
    handlers: {
      onEvent?: (ev: Record<string, unknown>) => void
      onReady?: (p: unknown) => void
    },
    since = 0,
  ) => {
    fake.subscribeSince.push(since)
    fake.onEvent = handlers.onEvent
    handlers.onReady?.({ status: { running: false } })
    return () => undefined
  },
}))

export { fake }
