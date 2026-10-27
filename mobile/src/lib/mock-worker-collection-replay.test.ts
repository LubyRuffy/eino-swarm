import { afterEach, expect, it } from "vitest"
import { MockHost, MockLink } from "./mock-link"
import { OpLog, OpStop, OpWatch, type RemoteEvent, type RemoteResponse } from "./rpc"

const prior = location.href
afterEach(() => history.replaceState(null, "", prior))

for (const order of ["after_terminal", "before_cleanup"]) {
  it(`supplies complete collection-decline and cancellation fixture receipts in the stopped fixture: ${order}`, async () => {
    history.replaceState(null, "", `/?mock=1&tick=0&history=worker-collection&collection=stopped&stop_order=${order}`)
    const host = new MockHost(0), link = new MockLink(host)
    try {
      const ready = await link.rpc({ op: OpWatch, thread_id: "t-live" })
      assertStop(ready.events ?? [], order)
      expect(ready.status?.running).toBe(false)
    } finally { link.close(); host.stop() }
  })
}

it("delivers the decline over live OpEvent before cold and paged replay", async () => {
  history.replaceState(null, "", "/?mock=1&tick=0&history=worker-collection&collection=stopping")
  const host = new MockHost(0), link = new MockLink(host), frames: RemoteResponse[] = []
  link.onPush = (frame) => frames.push(frame)
  try {
    const running = await link.rpc({ op: OpWatch, thread_id: "t-live" })
    expect(running.status?.running).toBe(true)
    await link.rpc({ op: OpStop, thread_id: "t-live" })
    const live = frames.flatMap((frame) => frame.event ? [frame.event] : [])
    expect(live.some((event) => event.kind === "workers_collection_declined")).toBe(true)
    assertStop([...(running.events ?? []), ...live], "after_terminal")
    const cold = await link.rpc({ op: OpWatch, thread_id: "t-live" })
    assertStop(cold.events ?? [], "after_terminal")
    history.replaceState(null, "", "/?mock=1&tick=0&history=worker-collection&collection=stopping&collection_page=1")
    const tail = await link.rpc({ op: OpWatch, thread_id: "t-live" })
    expect(tail.more).toBe(true)
    expect(tail.events?.some((event) => event.kind === "workers_collection_declined")).toBe(false)
    const earlier = await link.rpc({ op: OpLog, thread_id: "t-live", before: tail.events?.[0]?.seq })
    assertStop([...(earlier.events ?? []), ...(tail.events ?? [])], "after_terminal")
  } finally { link.close(); host.stop() }
})

function assertStop(events: RemoteEvent[], order: string) {
  const decline = events.find((event) => event.kind === "workers_collection_declined")
  expect(decline).toBeDefined()
  expect(JSON.parse(decline!.text)).toEqual({ wait_id: "collection-wait", worker_ids: ["worker-1"],
    workers: [{ agent_id: "worker-1", role: "worker", generation: 1, running: true }] })
  const cleanup = events.find((event) => event.kind === "cleanup")!
  const terminal = events.find((event) => event.kind === "error")!
  const finished = events.find((event) => event.kind === "finished")!
  expect(cleanup?.seq).toBeGreaterThan(decline!.seq)
  expect(terminal?.err).toBe("stopped before collecting worker results")
  expect(finished?.agent_id).toBe("worker-1")
  expect(finished?.err).toBe("swarm: agent worker-1 cancelled: context canceled")
  expect(finished?.seq)[order === "before_cleanup" ? "toBeLessThan" : "toBeGreaterThan"](order === "before_cleanup" ? cleanup.seq : terminal.seq)
}


it("keeps paging opt-in limited to the stopped collection fixture", async () => {
  for (const query of ["history=worker-collection&collection=stopped&collection_page=true", "history=other&collection=stopped&collection_page=1"]) {
    history.replaceState(null, "", "/?mock=1&tick=0&" + query)
    const host = new MockHost(0), link = new MockLink(host)
    try {
      const ready = await link.rpc({ op: OpWatch, thread_id: "t-live" })
      expect(ready.more).toBe(false)
    } finally { link.close(); host.stop() }
  }
})
