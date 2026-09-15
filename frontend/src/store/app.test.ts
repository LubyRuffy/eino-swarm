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
  steers: [] as Array<{ id: string; text: string }>,
  uploads: [] as string[],
  /** Which project each call was scoped to, so a sidebar filter and a new
   *  conversation can be checked to land in the same place. */
  listedProjects: [] as Array<string | undefined>,
  createdIn: [] as Array<string | undefined>,
  reviewed: [] as string[],
  loadedMemory: [] as string[],
  reviewCode: undefined as string | undefined,
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
      models: async () => ({ models: [], default: "default", mock: true }),
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
          memory: { text: "", entries: [], chars: 0, limit: 2200 },
          skills: [],
        }
      },
      reviewThread: async (id: string) => {
        fake.reviewed.push(id)
        if (fake.reviewCode) {
          throw new ApiError("nothing to review", 409, fake.reviewCode)
        }
        return { id: "tn_1" }
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
      }),
      turns: async () => [],
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      steer: async (id: string, text: string) => {
        fake.steers.push({ id, text })
        return { steered: false }
      },
      upload: async (id: string) => {
        fake.uploads.push(id)
        return []
      },
      interrupt: async () => ({ interrupted: true }),
    },
  }
})

vi.mock("@/lib/stream", () => ({
  subscribeEvents: (_id: string, handlers: { onReady?: (p: unknown) => void }) => {
    handlers.onReady?.({ status: { running: false } })
    return () => undefined
  },
}))

beforeEach(() => {
  fake.steers.length = 0
  fake.uploads.length = 0
  fake.created = 0
  fake.createDelayMs = 0
  fake.listedProjects.length = 0
  fake.createdIn.length = 0
  fake.reviewed.length = 0
  fake.loadedMemory.length = 0
  fake.reviewCode = undefined
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    error: undefined,
  })
  useProjects.setState({
    projects: [],
    selectedId: undefined,
    memory: undefined,
    memoryProjectId: undefined,
    error: undefined,
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
  it("filters the conversations and loads the memory of the chosen project", async () => {
    await useApp.getState().selectProject("pj_1")
    expect(useProjects.getState().selectedId).toBe("pj_1")
    expect(fake.listedProjects).toContain("pj_1")
    expect(fake.loadedMemory).toContain("pj_1")
  })

  // The selected project is where work is meant to go. A new conversation
  // that ignored it would run against the wrong directory with the wrong
  // instruction, which is the whole point of a project.
  it("starts a new conversation in the selected project", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().newThread()
    expect(fake.createdIn.at(-1)).toBe("pj_1")
  })

  it("lets a caller override the selected project", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().newThread("pj_other")
    expect(fake.createdIn.at(-1)).toBe("pj_other")
  })

  it("goes back to every conversation when the filter is cleared", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().selectProject(undefined)
    expect(useProjects.getState().selectedId).toBeUndefined()
    expect(fake.listedProjects.at(-1)).toBeUndefined()
  })

  it("asks for a review of the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().reviewNow()
    expect(fake.reviewed).toEqual(["th_old"])
    expect(useApp.getState().error).toBeUndefined()
  })

  // A conversation with nothing finished in it has nothing to review. That is
  // an answer, not a failure to put in front of the user.
  it("stays quiet when there is nothing to review", async () => {
    await useApp.getState().boot()
    fake.reviewCode = "idle"
    await useApp.getState().reviewNow()
    expect(useApp.getState().error).toBeUndefined()
  })

  it("reports a review that failed for any other reason", async () => {
    await useApp.getState().boot()
    fake.reviewCode = "busy"
    await useApp.getState().reviewNow()
    expect(useApp.getState().error).toBe("nothing to review")
  })

  it("does not review when no conversation is open", async () => {
    await useApp.getState().reviewNow()
    expect(fake.reviewed).toEqual([])
  })
})
