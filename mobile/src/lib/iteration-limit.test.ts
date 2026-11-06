import { afterEach, expect, it } from "vitest"
import { setLocale } from "./i18n"
import { applyEvent, type CompactBlock } from "./transcript"
import { applyPush, openView, prependOlder } from "./session"
import type { RemoteEvent } from "./rpc"

afterEach(() => setLocale("en"))
const event = (seq: number, kind: string, text = "", turn = "turn"): RemoteEvent => ({
  thread_id: "thread", turn_id: turn, seq, kind, text, created_at: "2026-11-06T10:00:00Z",
})
const cap = event(2, "max_iterations", '{"reason":"iterations","limit":7,"extend_by":30}')

it("keeps the manager limit visible and actionable without exposing JSON", () => {
  setLocale("zh")
  const blocks = applyEvent([], cap)
  expect(blocks).toHaveLength(1)
  expect(blocks[0].text).toContain("7")
  expect(blocks[0].text).toContain("30")
  expect(blocks[0].text).not.toContain("extend_by")
  expect(blocks[0]).toMatchObject({ turnId: "turn", confirm: { pending: true, limit: 7, extendBy: 30 } })
})

it("settles only the current turn limit and never revives its buttons on paging", () => {
  let view = openView({ id: "thread", title: "Task", running: { thread_id: "thread", title: "Task", turn_id: "turn" } })
  view = applyPush(view, { v: 1, id: "ready", ok: true, op: "ready", thread_id: "thread", events: [cap], seq: 2,
    status: { running: true, turn_id: "turn", awaiting_continue: true } })
  expect(view.detail?.running?.awaiting_continue).toBe(true)
  view = applyPush(view, { v: 1, id: "continued", ok: true, op: "event", event: event(3, "max_iterations_continued", "continuing") })
  expect(view.blocks[0].confirm).toMatchObject({ pending: false, continued: true })
  expect(view.detail?.running?.awaiting_continue).toBe(false)
  view = prependOlder(view, [event(1, "user_message", "Inspect the workspace.")], false)
  expect(view.blocks.find((b) => b.confirm)?.confirm?.pending).toBe(false)
})

it("keeps a newer snapshot authoritative when the cap is outside the loaded page", () => {
  const view = applyPush(openView({ id: "thread", title: "Task" }), {
    v: 1, id: "ready", ok: true, op: "ready", thread_id: "thread", events: [], seq: 50,
    status: { running: true, turn_id: "turn", awaiting_continue: true },
  })
  expect(view.detail?.running?.awaiting_continue).toBe(true)
})

it("lets a current nonpaused snapshot override a historical pending cap", () => {
  const view = applyPush(openView({ id: "thread", title: "Task" }), {
    v: 1, id: "ready", ok: true, op: "ready", thread_id: "thread", events: [cap], seq: 50,
    status: { running: true, turn_id: "turn", awaiting_continue: false },
  })
  expect(view.detail?.running?.awaiting_continue).toBe(false)
})

it("keeps automatic worker collection waiting distinct from a human decision", () => {
  let view = applyPush(openView({ id: "thread", title: "Task" }), {
    v: 1, id: "ready", ok: true, op: "ready", thread_id: "thread", events: [],
    status: { running: true, turn_id: "turn", awaiting_continue: false },
  })
  view = applyPush(view, { v: 1, id: "event", ok: true, op: "event", event:
    event(4, "max_iterations", '{"reason":"workers_uncollected","automatic_resume":true}') })
  expect(view.detail?.running?.awaiting_continue).toBe(false)
  expect(view.blocks[0].confirm).toBeUndefined()
})

it("settles the old limit at a terminal or new turn and ignores other turn receipts", () => {
  const blocks = applyEvent([], cap)
  expect(applyEvent(blocks, event(3, "max_iterations_continued", "", "other"))[0].confirm?.pending).toBe(true)
  for (const kind of ["done", "error", "turn"]) {
    expect(applyEvent(blocks, event(4, kind))[0].confirm?.pending).toBe(false)
  }
})

it("accepts a legacy limit but keeps malformed metadata out of the transcript", () => {
  expect(applyEvent([], event(1, "max_iterations", '{"limit":400}'))[0].confirm).toMatchObject({ limit: 400, pending: true })
  for (const text of ['{"reason":"other","limit":8}', '{"limit":"8"}', '{"limit":-1}', '{"limit":8,"extend_by":0}', '[]', '{']) {
    expect(applyEvent([], event(1, "max_iterations", text))).toEqual([])
  }
  const blocks = [cap, event(3, "done")].reduce<CompactBlock[]>((all, ev) => applyEvent(all, ev), [])
  expect(blocks[0].text).not.toContain("extend_by")
})
