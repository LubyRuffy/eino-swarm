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
