import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { api } from "@/lib/api"
import type { DistillResult } from "@/lib/distillation"
import { useDistillation, useWikiBuild } from "./distillation"
import { useKnowledge } from "./knowledge"

vi.mock("@/lib/api", () => ({
  api: { startDistillation: vi.fn(), distillStatus: vi.fn() },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

describe("project distillation panel state", () => {
  beforeEach(() => {
    useDistillation.setState({ projects: {} })
    useWikiBuild.setState({ projects: {} })
    useKnowledge.setState({ refreshByProject: {} })
  })
  afterEach(() => vi.resetAllMocks())

  it("keeps a newer admission when an earlier panel refresh returns", async () => {
    const fetch = deferred<DistillResult>()
    vi.mocked(api.distillStatus).mockReturnValueOnce(fetch.promise)
    vi.mocked(api.startDistillation).mockResolvedValueOnce({ state: "running", review_id: "new" })
    const { refresh, start } = useDistillation.getState()
    const pending = refresh("one", new AbortController().signal)
    await start("one", { scope: "project" })
    fetch.resolve({ state: "completed", review_id: "old" })
    await pending

    expect(useDistillation.getState().projects.one.result?.review_id).toBe("new")
    expect(useDistillation.getState().projects.two).toBeUndefined()
  })

  it("keeps checkpoint counts when the other panel's older poll arrives late", async () => {
    const old = deferred<DistillResult>()
    const latest: DistillResult = {
      state: "running", review_id: "saved",
      progress: { total: 10, processed: 4, remaining: 6, skipped: 0, batches: 1 },
    }
    vi.mocked(api.distillStatus).mockReturnValueOnce(old.promise).mockResolvedValueOnce(latest)
    const signal = new AbortController().signal
    const { refresh } = useDistillation.getState()
    const pending = refresh("one", signal)
    await refresh("one", signal)
    old.resolve({ ...latest, progress: { total: 10, processed: 0, remaining: 10, skipped: 0, batches: 0 } })
    await pending
    expect(useDistillation.getState().projects.one.result).toEqual(latest)
  })

  it("shares one request across panels while admission is pending and retains the terminal receipt", async () => {
    const post = deferred<DistillResult>()
    vi.mocked(api.startDistillation).mockReturnValueOnce(post.promise)
    const { refresh, start } = useDistillation.getState()
    const first = start("one", { scope: "thread", thread_id: "task", enable_knowledge: true })
    await start("one", { scope: "project" })
    expect(api.startDistillation).toHaveBeenCalledTimes(1)
    post.resolve({ state: "running", review_id: "shared" })
    await first

    const receipt: DistillResult = { state: "partial", review_id: "shared", error: "revision conflict" }
    vi.mocked(api.distillStatus).mockResolvedValueOnce(receipt)
    await refresh("one", new AbortController().signal)
    expect(useDistillation.getState().projects.one.result).toEqual(receipt)
  })

  it("refreshes the graph as soon as the local scan publishes it", async () => {
    const scanning: DistillResult = {
      state: "running", review_id: "scan", knowledge_enabled: true,
      wiki_phase: "scan", wiki_scan_processed: 0,
      progress: { total: 10, processed: 0, remaining: 10, skipped: 0, batches: 0 },
    }
    const ready: DistillResult = { ...scanning, wiki_phase: "graph", wiki_scan_processed: 10, wiki_scan_complete: true }
    vi.mocked(api.startDistillation).mockResolvedValueOnce(scanning)
    vi.mocked(api.distillStatus).mockResolvedValueOnce(ready)
    const { refresh, start } = useWikiBuild.getState()
    await start("one", { scope: "project" })
    const version = useKnowledge.getState().refreshByProject.one
    await refresh("one", new AbortController().signal)
    expect(useWikiBuild.getState().projects.one.result).toEqual(ready)
    expect(useKnowledge.getState().refreshByProject.one).toBe(version + 1)
  })
})
