import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"

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
  return {
    ApiError: class ApiError extends Error {},
    api: {
      meta: async () => ({ mode: "web", configured: true, capabilities: {} }),
      models: async () => ({ models: [], default: "default", mock: true }),
      threads: async () => [thread("th_old")],
      createThread: async () => {
        fake.created++
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
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
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
