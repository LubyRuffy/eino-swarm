import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { graphAPI } from "@/lib/graph/api"
import { publishGraphEvent } from "@/lib/graph/events"
import type { GraphView } from "@/lib/graph/types"
import { PROJECT_POLL_MS, useGraph } from "./graph"

vi.mock("@/lib/graph/api", () => ({ graphAPI: { thread: vi.fn(), project: vi.fn() } }))

const view = (revision: number, owner = "o"): GraphView =>
  ({ owner_id: owner, revision, subjects: [], links: [], todos: [], total: 0, more: false })
const deferred = <T,>() => {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

beforeEach(() => {
  useGraph.setState({ owner: undefined, view: undefined, loading: false, connected: false, error: undefined })
})
afterEach(() => vi.resetAllMocks())

describe("the graph store", () => {
  it("reads once on connect and again when a newer revision is announced", async () => {
    vi.mocked(graphAPI.thread).mockResolvedValueOnce(view(1)).mockResolvedValueOnce(view(2))
    const close = useGraph.getState().connect({ threadId: "th" })
    await settle()
    expect(useGraph.getState().view?.revision).toBe(1)
    publishGraphEvent("th", { kind: "revision", revision: 2 })
    await settle()
    expect(useGraph.getState().view?.revision).toBe(2)
    expect(graphAPI.thread).toHaveBeenCalledTimes(2)
    close()
  })

  // A frame is a hint to refetch; a duplicate or old one must not cost a request.
  it("ignores a revision it already has", async () => {
    vi.mocked(graphAPI.thread).mockResolvedValue(view(3))
    const close = useGraph.getState().connect({ threadId: "th" })
    await settle()
    publishGraphEvent("th", { kind: "revision", revision: 3 })
    publishGraphEvent("th", { kind: "revision", revision: 1 })
    await settle()
    expect(graphAPI.thread).toHaveBeenCalledTimes(1)
    close()
  })

  it("does not roll back to an older read that lost a race", async () => {
    const slow = deferred<GraphView>()
    vi.mocked(graphAPI.thread).mockReturnValueOnce(slow.promise)
    const close = useGraph.getState().connect({ threadId: "th" })
    useGraph.setState({ view: view(5) })
    slow.resolve(view(4))
    await settle()
    expect(useGraph.getState().view?.revision).toBe(5)
    close()
  })

  it("drops a read that finishes after the panel moved to another conversation", async () => {
    const first = deferred<GraphView>()
    vi.mocked(graphAPI.thread).mockReturnValueOnce(first.promise).mockResolvedValueOnce(view(1, "two"))
    useGraph.getState().connect({ threadId: "one" })
    const close = useGraph.getState().connect({ threadId: "two" })
    await settle()
    first.resolve(view(9, "one"))
    await settle()
    expect(useGraph.getState().view?.owner_id).toBe("two")
    close()
  })

  it.each([
    { revision: 0, oldReadFirst: true },
    { revision: 8, oldReadFirst: false },
  ])("refreshes after the same conversation moves to an owner at revision $revision", async ({ revision, oldReadFirst }) => {
    const moved = deferred<GraphView>()
    const previous = deferred<GraphView>()
    const api = vi.mocked(graphAPI.thread)
    api.mockResolvedValueOnce(view(8, "before")).mockReturnValueOnce(previous.promise)
      .mockReturnValueOnce(moved.promise).mockResolvedValueOnce(view(revision + 1, "after"))
    const close = useGraph.getState().connect({ threadId: "moving" })
    try {
      await settle()
      publishGraphEvent("moving", { kind: "revision", ownerId: "before", revision: 9 })
      publishGraphEvent("moving", { kind: "revision", ownerId: "after", revision })
      expect(useGraph.getState().view).toBeUndefined()
      expect(api).toHaveBeenCalledTimes(3)
      expect(api.mock.calls[1][1]?.aborted).toBe(true)
      if (oldReadFirst) {
        previous.resolve(view(9, "before"))
        await settle()
        expect(useGraph.getState().view).toBeUndefined()
      }
      moved.resolve(view(revision, "after"))
      await settle()
      if (!oldReadFirst) {
        previous.resolve(view(9, "before"))
        await settle()
      }
      expect(useGraph.getState().view).toEqual(view(revision, "after"))
      publishGraphEvent("moving", { kind: "revision", ownerId: "after", revision: revision + 1 })
      await settle()
      expect(useGraph.getState().view).toEqual(view(revision + 1, "after"))
      expect(api).toHaveBeenCalledTimes(4)
    } finally {
      close()
    }
  })

  it("waits for the next owner announcement if a snapshot is already from a third owner", async () => {
    const api = vi.mocked(graphAPI.thread)
    api.mockResolvedValueOnce(view(8, "before")).mockResolvedValueOnce(view(3, "after"))
      .mockResolvedValueOnce(view(3, "after"))
    const close = useGraph.getState().connect({ threadId: "moving" })
    try {
      await settle()
      // The conversation moved again between the second owner's SSE frame and
      // the snapshot request. Only a frame for the third owner can admit it.
      publishGraphEvent("moving", { kind: "revision", ownerId: "middle", revision: 0 })
      await settle()
      expect(useGraph.getState().view).toBeUndefined()
      publishGraphEvent("moving", { kind: "revision", ownerId: "after", revision: 3 })
      await settle()
      expect(useGraph.getState().view).toEqual(view(3, "after"))
      expect(api).toHaveBeenCalledTimes(3)
    } finally {
      close()
    }
  })

  it("keeps the last picture and reports a failed read", async () => {
    vi.mocked(graphAPI.thread).mockResolvedValueOnce(view(1)).mockRejectedValueOnce(new Error("offline"))
    const close = useGraph.getState().connect({ threadId: "th" })
    await settle()
    await useGraph.getState().refresh()
    expect(useGraph.getState().view?.revision).toBe(1)
    expect(useGraph.getState().error).toBe("offline")
    close()
  })

  // A failed first read has no picture to compare with; retrying on its own would loop forever.
  it("stops requesting when a newer revision's snapshot keeps failing", async () => {
    vi.mocked(graphAPI.thread).mockResolvedValueOnce(view(1)).mockRejectedValue(new Error("unavailable"))
    const close = useGraph.getState().connect({ threadId: "th" })
    await settle()
    publishGraphEvent("th", { kind: "revision", revision: 2 })
    await settle()
    await settle()
    expect(graphAPI.thread).toHaveBeenCalledTimes(2)
    expect(useGraph.getState().view?.revision).toBe(1)
    expect(useGraph.getState().error).toBe("unavailable")
    close()
  })

  it("consumes a newer revision received during a failed fetch", async () => {
    vi.useFakeTimers()
    const api = vi.mocked(graphAPI.thread)
    let rejectFetch!: (error: Error) => void
    const failing = new Promise<GraphView>((_, reject) => { rejectFetch = reject })
    api.mockResolvedValueOnce(view(1)).mockReturnValueOnce(failing).mockResolvedValue(view(3))
    const close = useGraph.getState().connect({ threadId: "th" })
    try {
      await vi.advanceTimersByTimeAsync(1)
      publishGraphEvent("th", { kind: "revision", revision: 2 })
      expect(api).toHaveBeenCalledTimes(2)
      publishGraphEvent("th", { kind: "revision", revision: 3 })
      rejectFetch(new Error("one temporary failure"))
      await vi.advanceTimersByTimeAsync(30000)
      expect(useGraph.getState().view?.revision).toBe(3)
      expect(api).toHaveBeenCalledTimes(3)
    } finally {
      close()
      vi.useRealTimers()
    }
  })

  it("retries a single unavailable snapshot once without another frame", async () => {
    vi.useFakeTimers()
    const api = vi.mocked(graphAPI.thread)
    api.mockResolvedValueOnce(view(1)).mockRejectedValueOnce(new Error("unavailable")).mockResolvedValue(view(2))
    const close = useGraph.getState().connect({ threadId: "th" })
    try {
      await vi.advanceTimersByTimeAsync(1)
      publishGraphEvent("th", { kind: "revision", revision: 2 })
      await vi.advanceTimersByTimeAsync(30000)
      expect(useGraph.getState().view?.revision).toBe(2)
      expect(api).toHaveBeenCalledTimes(3)
    } finally {
      close()
      vi.useRealTimers()
    }
  })

  it("does not retry a failed first read by itself", async () => {
    vi.mocked(graphAPI.thread).mockRejectedValue(new Error("offline"))
    const close = useGraph.getState().connect({ threadId: "th" })
    await settle()
    await settle()
    expect(graphAPI.thread).toHaveBeenCalledTimes(1)
    expect(useGraph.getState().error).toBe("offline")
    close()
  })

  it("polls a project that has no conversation connection to ride", async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(graphAPI.project).mockResolvedValue(view(1))
      const close = useGraph.getState().connect({ projectId: "pj" })
      await vi.advanceTimersByTimeAsync(PROJECT_POLL_MS * 2 + 10)
      expect(vi.mocked(graphAPI.project).mock.calls.length).toBeGreaterThanOrEqual(3)
      close()
      const calls = vi.mocked(graphAPI.project).mock.calls.length
      await vi.advanceTimersByTimeAsync(PROJECT_POLL_MS * 2)
      expect(graphAPI.project).toHaveBeenCalledTimes(calls)
    } finally {
      vi.useRealTimers()
    }
  })
})
