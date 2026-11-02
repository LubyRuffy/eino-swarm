import { afterEach, describe, expect, it, vi } from "vitest"
import { nuwaAPI, subscribeNuwa } from "./nuwa-api"

class Source {
  static last: Source
  onopen?: () => void
  onerror?: () => void
  listener?: (event: MessageEvent) => void
  closed = false
  constructor(readonly url: string) { Source.last = this }
  addEventListener(name: string, listener: (event: MessageEvent) => void) {
    expect(name).toBe("nuwa_updated")
    this.listener = listener
  }
  close() { this.closed = true }
  send(data: string) { this.listener?.({ data } as MessageEvent) }
}

afterEach(() => vi.unstubAllGlobals())

describe("Nuwa project transport", () => {
  it("reads the authoritative snapshot and retains API refusal details", async () => {
    const fetch = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ project_id: "a", revision: 2, nodes: [], workers: [] })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: "Project unavailable", code: "project_not_found" }), { status: 404 }))
    vi.stubGlobal("fetch", fetch)
    expect((await nuwaAPI.snapshot("a")).revision).toBe(2)
    expect(fetch).toHaveBeenCalledWith("/api/projects/a/nuwa")
    await expect(nuwaAPI.snapshot("a")).rejects.toMatchObject({ message: "Project unavailable", status: 404, code: "project_not_found" })
  })

  it("subscribes by project identity, rejects malformed notifications, and closes its own source", () => {
    vi.stubGlobal("EventSource", Source)
    const handlers = { onRevision: vi.fn(), onOpen: vi.fn(), onError: vi.fn() }
    const stop = subscribeNuwa("a", handlers)
    expect(Source.last.url).toBe("/api/projects/a/nuwa/events")
    Source.last.onopen?.()
    Source.last.onerror?.()
    Source.last.send("broken")
    Source.last.send(JSON.stringify({ project_id: "b", revision: 9 }))
    Source.last.send(JSON.stringify({ project_id: "a", revision: "9" }))
    Source.last.send(JSON.stringify({ project_id: "a", revision: -1 }))
    Source.last.send(JSON.stringify({ project_id: "a", revision: 2 }))
    expect(handlers.onRevision).toHaveBeenCalledOnce()
    expect(handlers.onRevision).toHaveBeenCalledWith(2)
    expect(handlers.onOpen).toHaveBeenCalledOnce()
    expect(handlers.onError).toHaveBeenCalledOnce()
    stop()
    expect(Source.last.closed).toBe(true)
    Source.last.send(JSON.stringify({ project_id: "a", revision: 3 }))
    Source.last.onopen?.()
    expect(handlers.onRevision).toHaveBeenCalledOnce()
    expect(handlers.onOpen).toHaveBeenCalledOnce()
  })
})
