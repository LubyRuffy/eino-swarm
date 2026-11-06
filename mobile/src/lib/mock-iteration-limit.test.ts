import { afterEach, expect, it } from "vitest"
import { MockHost, MockLink } from "./mock-link"
import { OpOpen, OpWatch, type RemoteResponse } from "./rpc"

const originalURL = location.href
afterEach(() => history.replaceState(null, "", originalURL))

for (const proceed of [true, false]) {
  it(`keeps the original paused turn when the phone chooses continue=${proceed}`, async () => {
    history.replaceState(null, "", "/?mock=1&tick=0&history=iteration-limit")
    const host = new MockHost(0)
    const link = new MockLink(host)
    const frames: RemoteResponse[] = []
    link.onPush = (frame) => frames.push(frame)
    try {
      const ready = await link.rpc({ op: OpWatch, thread_id: "t-live", include_detail: true })
      expect(ready.status).toMatchObject({ running: true, awaiting_continue: true })
      expect(ready.detail?.running?.awaiting_continue).toBe(true)
      expect(ready.events?.find((event) => event.kind === "max_iterations")).toMatchObject({ turn_id: "iteration-turn", text: '{"limit":6,"extend_by":24}' })
      expect(await link.rpc({ op: "continue", thread_id: "t-live", continue: proceed })).toMatchObject({ ok: true })
      expect(frames.flatMap((frame) => frame.event ? [frame.event] : []).every((event) => event.turn_id === "iteration-turn")).toBe(true)
      expect(frames.some((frame) => frame.event?.kind === "user_message")).toBe(false)
      expect(frames.some((frame) => frame.event?.kind === "max_iterations_continued")).toBe(proceed)
      expect(frames.at(-1)?.event).toMatchObject(proceed ? { kind: "done" } : { kind: "error", err: "interrupted" })
      expect((await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail?.running).toBeUndefined()
      expect((await link.rpc({ op: "continue", thread_id: "t-live", continue: true })).ok).toBe(false)
    } finally { link.close(); host.stop() }
  })
}

it("keeps a rejected continuation paused and accepts a later explicit choice", async () => {
  history.replaceState(null, "", "/?mock=1&tick=0&history=iteration-limit&continuefail=1")
  const host = new MockHost(0)
  const link = new MockLink(host)
  try {
    expect(await link.rpc({ op: "continue", thread_id: "t-live" })).toMatchObject({ ok: false, code: "bad_request" })
    expect(await link.rpc({ op: "continue", thread_id: "missing", continue: false })).toMatchObject({ ok: false, code: "not_found" })
    expect(await link.rpc({ op: "continue", thread_id: "t-live", continue: true })).toMatchObject({ ok: false, error: "PC rejected continuation" })
    expect((await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail?.running?.awaiting_continue).toBe(true)
    expect(await link.rpc({ op: "continue", thread_id: "t-live", continue: false })).toMatchObject({ ok: true })
  } finally { link.close(); host.stop() }
})

it("keeps the live pause status when the snapshot contains no cap event", async () => {
  history.replaceState(null, "", "/?mock=1&tick=0&history=iteration-limit&cap_snapshot=1")
  const host = new MockHost(0)
  const link = new MockLink(host)
  try {
    const ready = await link.rpc({ op: OpWatch, thread_id: "t-live", include_detail: true })
    expect(ready.events).toEqual([])
    expect(ready.status).toMatchObject({ running: true, awaiting_continue: true, turn_id: "iteration-turn" })
    expect(await link.rpc({ op: "continue", thread_id: "t-live", continue: true })).toMatchObject({ ok: true })
  } finally { link.close(); host.stop() }
})
