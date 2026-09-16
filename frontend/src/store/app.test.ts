import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

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
  createdIn: [] as Array<string | undefined>,
  reviewed: [] as string[],
  loadedMemory: [] as string[],
  reviewCode: undefined as string | undefined,
  onEvent: undefined as ((ev: Record<string, unknown>) => void) | undefined,
  discovered: [] as Array<Record<string, unknown>>,
  savedProviders: undefined as unknown,
  savedUI: undefined as { locale?: string } | undefined,
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
  enqueueIdle: false,
  goals: [] as Array<{ id: string; goal?: string; goal_edit?: boolean; goal_resume?: boolean }>,
  compacts: [] as string[],
  reordered: [] as Array<{ ids: string[]; projectId?: string }>,
  reorderFail: false,
}))

vi.mock("@/lib/api", () => {
  const thread = (id: string) => ({
    id,
    title: "New conversation",
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
      meta: async () => ({ mode: "web", configured: true, capabilities: {} }),
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
        return { models: ["alpha", "beta"], context_windows: { alpha: 128000 } }
      },
      saveSettings: async (patch: {
        models?: { providers?: unknown }
        ui?: { locale?: string }
      }) => {
        fake.savedProviders = patch.models?.providers
        if (patch.ui) fake.savedUI = patch.ui
        return patch
      },
      threads: async (_archived?: boolean, projectId?: string) => {
        fake.listedProjects.push(projectId)
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
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      followups: async () => fake.queuedItems,
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
  ) => {
    fake.onEvent = handlers.onEvent
    handlers.onReady?.({ status: { running: false } })
    return () => undefined
  },
}))

beforeEach(() => {
  fake.steers.length = 0
  fake.started.length = 0
  fake.enqueued.length = 0
  fake.queuedItems.length = 0
  fake.steeredFollowups.length = 0
  fake.enqueueIdle = false
  fake.uploads.length = 0
  fake.continues.length = 0
  fake.created = 0
  fake.createDelayMs = 0
  fake.listedProjects.length = 0
  fake.createdIn.length = 0
  fake.reviewed.length = 0
  fake.loadedMemory.length = 0
  fake.reviewCode = undefined
  fake.onEvent = undefined
  fake.discovered.length = 0
  fake.savedProviders = undefined
  fake.savedUI = undefined
  fake.listedModels = []
  fake.goals.length = 0
  fake.compacts.length = 0
  fake.reordered.length = 0
  fake.reorderFail = false
  fake.threadUsage = undefined
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    followups: [],
    error: undefined,
  })
  useProjects.setState({
    projects: [],
    selectedId: undefined,
    memory: undefined,
    memoryProjectId: undefined,
    error: undefined,
    reviewing: false,
    reviewHint: undefined,
    pendingReviewTurnId: undefined,
  })
})

describe("send", () => {
  it("creates a conversation when there is none", async () => {
    await useApp.getState().send("do the thing")
    expect(fake.steers).toEqual([{ id: "th_new_1", text: "do the thing" }])
  })

  it("waits for a conversation that is still being created", async () => {
    await useApp.getState().boot()
    expect(useApp.getState().activeId).toBe("th_old")

    // The click and the Enter happen before the POST comes back. Sending to
    // the conversation the user just left would run the turn out of sight.
    fake.createDelayMs = 20
    const opening = useApp.getState().newThread()
    await useApp.getState().send("in the new one")
    await opening

    expect(fake.steers).toEqual([{ id: "th_new_1", text: "in the new one" }])
    expect(useApp.getState().activeId).toBe("th_new_1")
  })

  it("shows the turn as running without waiting for the server to say so", async () => {
    await useApp.getState().send("go")
    expect(useApp.getState().status.running).toBe(true)
  })

  it("forwards pasted images on the same send as the caption", async () => {
    const images = [{ name: "clip.png", mime: "image/png", data: "AQID" }]
    await useApp.getState().send("look", images)
    expect(fake.steers).toEqual([{ id: "th_new_1", text: "look", images }])
  })

  it("injects a pasted image instead of queuing it", async () => {
    const images = [{ name: "clip.png", mime: "image/png", data: "AQID" }]
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("look", images)
    expect(fake.enqueued).toEqual([])
    expect(fake.steers).toEqual([{ id: "th_old", text: "look", images }])
  })

  it("injects a workspace file instead of queuing a text-only follow-up", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("what is this", undefined, {
      files: ["uploads/current.csv"],
    })
    expect(fake.enqueued).toEqual([])
    expect(fake.steers).toEqual([
      { id: "th_old", text: "what is this", files: ["uploads/current.csv"] },
    ])
  })

  it("queues a follow-up while a turn is running", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("after this finishes")
    expect(fake.enqueued).toEqual([{ id: "th_old", text: "after this finishes" }])
    expect(fake.steers).toEqual([])
    expect(useApp.getState().followups.map((f) => f.text)).toEqual([
      "after this finishes",
    ])
  })

  it("rewinds from a user message instead of steering or queuing", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [
        {
          id: "fu_1",
          thread_id: "th_old",
          seq: 1,
          text: "later",
          created_at: "",
        },
      ],
      transcript: {
        agentOrder: ["manager"],
        agents: {
          manager: {
            id: "manager",
            role: "manager",
            status: "done",
            activity: "",
            blocks: [
              {
                id: "b1",
                kind: "user",
                agentId: "manager",
                text: "first",
                turnId: "tn_1",
                seq: 1,
                at: "",
              },
              {
                id: "b2",
                kind: "user",
                agentId: "manager",
                text: "second",
                turnId: "tn_2",
                seq: 4,
                at: "",
              },
            ],
          },
        },
        turns: [
          { id: "tn_1", userText: "first", status: "done", agentIds: [] },
          { id: "tn_2", userText: "second", status: "running", agentIds: [] },
        ],
        lastSeq: 9,
        running: true,
      },
    })
    await useApp.getState().send("edited", undefined, { fromEventSeq: 4 })
    expect(fake.enqueued).toEqual([])
    expect(fake.steers).toEqual([])
    expect(fake.started).toEqual([{ id: "th_old", text: "edited", fromEventSeq: 4 }])
    expect(useApp.getState().transcript.agents.manager.blocks.map((b) => b.text)).toEqual([
      "first",
      "edited",
    ])
    expect(useApp.getState().transcript.running).toBe(true)
    expect(useApp.getState().followups).toEqual([])
  })

  it("injects immediately when asked to steer", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("narrow it", undefined, { steer: true })
    expect(fake.steers).toEqual([{ id: "th_old", text: "narrow it" }])
    expect(fake.enqueued).toEqual([])
  })

  it("starts a turn if the queue request finds nothing running", async () => {
    fake.enqueueIdle = true
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("the race ended")
    expect(fake.steers).toEqual([{ id: "th_old", text: "the race ended" }])
  })
})

describe("upload", () => {
  it("waits for a conversation that is still being created", async () => {
    await useApp.getState().boot()
    fake.createDelayMs = 20
    const opening = useApp.getState().newThread()
    await useApp.getState().upload([new File(["x"], "brief.txt")])
    await opening

    expect(fake.uploads).toEqual(["th_new_1"])
  })
})

describe("projects", () => {
  it("loads the memory of the chosen project without filtering the list", async () => {
    await useApp.getState().selectProject("pj_1")
    expect(useProjects.getState().selectedId).toBe("pj_1")
    expect(fake.listedProjects).not.toContain("pj_1")
    expect(fake.loadedMemory).toContain("pj_1")
  })

  // Top New conversation is Recents. A project folder's own control is
  // what lands work in that directory.
  it("starts a new conversation outside any project so it lands in Recents", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().newThread()
    expect(fake.createdIn.at(-1)).toBeUndefined()
  })

  it("lets a caller override the selected project", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().newThread("pj_other")
    expect(fake.createdIn.at(-1)).toBe("pj_other")
  })

  it("pins a conversation so the sidebar can track it", async () => {
    await useApp.getState().boot()
    await useApp.getState().pinThread("th_old", true)
    expect(useApp.getState().threads.find((th) => th.id === "th_old")?.pinned).toBe(
      true,
    )
    await useApp.getState().pinThread("th_old", false)
    expect(useApp.getState().threads.find((th) => th.id === "th_old")?.pinned).toBe(
      false,
    )
  })

  it("clears the selected project without re-listing", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().selectProject(undefined)
    expect(useProjects.getState().selectedId).toBeUndefined()
  })

  it("asks for a review of the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().reviewNow()
    expect(fake.reviewed).toEqual(["th_old"])
    expect(useApp.getState().error).toBeUndefined()
    expect(useProjects.getState().reviewing).toBe(true)
    expect(useProjects.getState().pendingReviewTurnId).toBe("tn_1")
  })

  // A conversation with nothing finished in it has nothing to review. That is
  // an answer on the panel they clicked, not a banner that looks like a crash.
  it("says so on the panel when there is nothing to review", async () => {
    await useApp.getState().boot()
    fake.reviewCode = "idle"
    await useApp.getState().reviewNow()
    expect(useApp.getState().error).toBeUndefined()
    expect(useProjects.getState().reviewing).toBe(false)
    expect(useProjects.getState().reviewHint).toMatch(/nothing to review/i)
  })

  it("reports a review that failed for any other reason", async () => {
    await useApp.getState().boot()
    fake.reviewCode = "busy"
    await useApp.getState().reviewNow()
    expect(useApp.getState().error).toBe("nothing to review")
    expect(useProjects.getState().reviewing).toBe(false)
  })

  it("does not review when no conversation is open", async () => {
    await useApp.getState().reviewNow()
    expect(fake.reviewed).toEqual([])
    expect(useProjects.getState().reviewHint).toMatch(/open a conversation/i)
  })
})

describe("extendTurn", () => {
  it("answers the tool-round cap on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().extendTurn(true)
    expect(fake.continues).toEqual([{ id: "th_old", proceed: true }])
  })

  it("does nothing when no conversation is open", async () => {
    await useApp.getState().extendTurn(false)
    expect(fake.continues).toEqual([])
  })
})

describe("goal and compact", () => {
  beforeEach(() => {
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal("cancelAnimationFrame", () => undefined)
  })

  it("patches the standing objective onto the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().setGoal("keep going")
    expect(fake.goals).toEqual([{ id: "th_old", goal: "keep going" }])
    expect(useApp.getState().threads[0]?.goal).toBe("keep going")
  })

  it("edits the standing objective in place", async () => {
    await useApp.getState().boot()
    await useApp.getState().editGoal("keep going, tighter")
    expect(fake.goals).toEqual([
      { id: "th_old", goal: "keep going, tighter", goal_edit: true },
    ])
  })

  it("resumes a standing objective on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().resumeGoal()
    expect(fake.goals).toEqual([{ id: "th_old", goal_resume: true }])
    expect(useApp.getState().status.running).toBe(true)
  })

  it("does not edit or resume when nothing is open", async () => {
    await useApp.getState().editGoal("x")
    await useApp.getState().resumeGoal()
    expect(fake.goals).toEqual([])
  })

  it("folds replay on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().compactThread()
    expect(fake.compacts).toEqual(["th_old"])
    expect(useApp.getState().threads[0]?.compacted).toBe(true)
  })

  it("does not compact when nothing is open", async () => {
    await useApp.getState().compactThread()
    expect(fake.compacts).toEqual([])
  })

  it("updates the banner from a goal event", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal).toBe("keep going")
    expect(useApp.getState().threads[0]?.goal_complete).toBe(false)
  })

  it("marks the objective complete from the stream", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_complete",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "{}",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal_complete).toBe(true)
  })

  it("marks the objective blocked from the stream", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_blocked",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: '{"reason":"needs an external change"}',
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal_blocked).toBe(true)
    expect(useApp.getState().threads[0]?.goal_block_reason).toBe("needs an external change")
  })

  it("keeps status flags when the objective is edited", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "goal",
      seq: 50,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going",
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_blocked",
      seq: 51,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: '{"reason":"needs an external change"}',
      created_at: new Date().toISOString(),
    })
    fake.onEvent?.({
      kind: "goal_edited",
      seq: 52,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: "keep going, tighter",
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().threads[0]?.goal).toBe("keep going, tighter")
    expect(useApp.getState().threads[0]?.goal_blocked).toBe(true)
  })
})

describe("a generated conversation title", () => {
  beforeEach(() => {
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal("cancelAnimationFrame", () => undefined)
  })

  function titleEvent(patch: Record<string, unknown> = {}) {
    return {
      kind: "title",
      seq: 41,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "title-namer",
      text: "Weekly status",
      created_at: new Date().toISOString(),
      ...patch,
    }
  }

  it("renames the open conversation from the title event", async () => {
    await useApp.getState().boot()
    expect(useApp.getState().threads[0]?.title).toBe("New conversation")
    fake.onEvent?.(titleEvent())
    expect(useApp.getState().threads[0]?.title).toBe("Weekly status")
  })

  it("does not rename from a namer that failed", async () => {
    await useApp.getState().boot()
    fake.onEvent?.(titleEvent({ err: "endpoint down" }))
    expect(useApp.getState().threads[0]?.title).toBe("New conversation")
  })
})

describe("refreshCatalogs", () => {
  it("re-lists every endpoint with a URL and does not reboot the conversation", async () => {
    fake.listedModels = [
      {
        id: "default\tbeta",
        provider_id: "default",
        provider_label: "Main",
        label: "beta",
        model: "beta",
        ready: true,
      },
    ]
    await useApp.getState().refreshCatalogs()
    expect(fake.discovered).toEqual([
      { provider_id: "default", base_url: "http://endpoint.invalid/v1" },
    ])
    const saved = fake.savedProviders as Array<{
      id: string
      catalog: string[]
      api_key?: string
    }>
    expect(saved.map((p) => p.id)).toEqual(["default", "blank"])
    expect(saved[0].catalog).toEqual(["alpha", "beta"])
    expect(saved[0]).toMatchObject({ model_context: { alpha: 128000 } })
    expect(saved[0].api_key).toBeUndefined()
    expect(useApp.getState().models).toEqual(fake.listedModels)
    expect(useApp.getState().activeId).toBeUndefined()
  })
})

describe("token usage", () => {
  beforeEach(() => {
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal("cancelAnimationFrame", () => undefined)
  })

  it("updates the meter from a live usage event", async () => {
    await useApp.getState().boot()
    fake.onEvent?.({
      kind: "usage",
      seq: 0,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: JSON.stringify({
        context_tokens: 71300,
        context_window: 256000,
        turn: { prompt_tokens: 8, completion_tokens: 2, total_tokens: 10, calls: 1 },
        thread: { total_tokens: 10, calls: 1 },
      }),
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().usage?.context_tokens).toBe(71300)
    expect(useApp.getState().usage?.context_window).toBe(256000)
  })

  it("loads the snapshot when opening a conversation", async () => {
    fake.threadUsage = {
      context_tokens: 90,
      context_window: 128000,
      turn: { prompt_tokens: 90, completion_tokens: 10, total_tokens: 100, calls: 1 },
      thread: { prompt_tokens: 90, completion_tokens: 10, total_tokens: 100, calls: 1 },
    }
    await useApp.getState().boot()
    expect(useApp.getState().usage?.context_tokens).toBe(90)
  })
})

describe("sidebar order", () => {
  it("pins a dragged conversation order", async () => {
    useApp.setState({
      threads: [
        {
          id: "th_1",
          title: "A",
          project_id: "",
          provider_id: "default",
          reasoning_effort: "",
          archived: false,
          created_at: "",
          last_active_at: "",
          running: false,
        },
        {
          id: "th_2",
          title: "B",
          project_id: "",
          provider_id: "default",
          reasoning_effort: "",
          archived: false,
          created_at: "",
          last_active_at: "",
          running: false,
        },
      ],
    })
    await useApp.getState().reorderThreads(["th_2", "th_1"])
    expect(fake.reordered).toEqual([{ ids: ["th_2", "th_1"], projectId: undefined }])
    expect(useApp.getState().threads.map((t) => t.id)).toEqual(["th_2", "th_1"])
  })

  it("reloads the list when a reorder fails", async () => {
    fake.reorderFail = true
    useApp.setState({
      threads: [
        {
          id: "th_1",
          title: "A",
          project_id: "",
          provider_id: "default",
          reasoning_effort: "",
          archived: false,
          created_at: "",
          last_active_at: "",
          running: false,
        },
      ],
    })
    await useApp.getState().reorderThreads(["th_1"])
    expect(useApp.getState().error).toMatch(/could not pin the order/)
    expect(useApp.getState().threads.map((t) => t.id)).toEqual(["th_old"])
  })
})

describe("locale preference", () => {
  // Desktop binds a random loopback, so localStorage-only would forget the
  // language on every launch. The pin has to ride PUT /api/settings.
  it("writes the chrome language through settings so the next boot keeps it", async () => {
    await useApp.getState().setLocale("zh")
    expect(useApp.getState().locale).toBe("zh")
    expect(document.documentElement.lang).toBe("zh-CN")
    expect(fake.savedUI).toEqual({ locale: "zh" })
  })

  it("applies a boot locale without rewriting settings", () => {
    useApp.getState().setLocale("zh", { persist: false })
    expect(useApp.getState().locale).toBe("zh")
    expect(fake.savedUI).toBeUndefined()
  })
})
