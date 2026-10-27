import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, expect, it } from "vitest"
import { ThreadLog } from "@/components/thread-blocks"
import { setLocale } from "./i18n"
import { OpEvent, OpReady, type RemoteEvent } from "./rpc"
import { applyPush, openView, prependOlder } from "./session"
import { managerBlocks, phoneAgents } from "./transcript"

// The forwarded receipt remains diagnostic evidence through every phone route.
const receipt = JSON.stringify({ wait_id: "collection-wait", worker_ids: ["worker-1"],
  workers: [{ agent_id: "worker-1", role: "worker", generation: 1, running: true }] })

function row(seq: number, kind: string, fields: Partial<RemoteEvent> = {}): RemoteEvent {
  return { thread_id: "thread", turn_id: "turn", agent_id: "manager", seq, kind, text: "", created_at: "2026-01-01T00:00:00Z", ...fields }
}

function stoppedEvents() {
  return [
    row(1, "user_message", { text: "Inspect the assigned inputs." }),
    row(5, "spawned", { agent_id: "worker-1", role: "worker" }),
    row(8, "reasoning", { agent_id: "worker-1", text: "Inspecting the assigned inputs." }),
    row(13, "max_iterations", { text: '{"reason":"workers_uncollected","automatic_resume":true}' }),
    row(14, "workers_collection_declined", { text: receipt }),
    row(16, "cleanup", { text: "Stopped unfinished workers." }),
    row(17, "error", { err: "stopped before collecting worker results" }),
    row(19, "finished", { agent_id: "worker-1", err: "swarm: agent worker-1 cancelled: context canceled" }),
  ]
}

function ready(events: RemoteEvent[], more = false) {
  return { v: 1, id: "", ok: true, op: OpReady, thread_id: "thread", events, seq: 19, more, status: { running: false } }
}

function routes(finishFirst: boolean) {
  const events = stoppedEvents()
  const detail = { id: "thread", title: "Work" }
  let live = applyPush(openView(detail), { ...ready([]), seq: 0, status: { running: true, turn_id: "turn" } })
  const delivery = finishFirst ? [...events.slice(0, 4), events[7], ...events.slice(4, 7)] : events
  for (const event of delivery) live = applyPush(live, { v: 1, id: "", ok: true, op: OpEvent, thread_id: "thread", event, ...(event.kind === "error" ? { status: { running: false } } : {}) })
  const cold = applyPush(openView(detail), ready(events))
  const tail = applyPush(openView(detail), ready(events.slice(-2), true))
  const middle = prependOlder(tail, events.slice(4, -2), true)
  const paged = prependOlder(middle, events.slice(0, 4), false)
  return { live, cold, paged }
}

afterEach(() => { cleanup(); setLocale("en") })

for (const language of ["en", "zh"] as const) {
  for (const finishFirst of [false, true]) {
    it(`keeps the collection decline internal across phone live/cold/paged replay: ${language}, late-finish first=${finishFirst}`, () => {
      setLocale(language)
      for (const view of Object.values(routes(finishFirst))) {
        expect(view.detail?.running).toBeUndefined()
        expect(view.events.find((event) => event.seq === 14)).toMatchObject({ kind: "workers_collection_declined", text: receipt })
        expect(view.events.filter((event) => event.seq === 14)).toHaveLength(1)
        expect(view.blocks.some((block) => block.text === receipt)).toBe(false)
        const agents = phoneAgents(view.blocks)
        expect(agents).toHaveLength(1)
        expect(agents[0]).toMatchObject({ id: "worker-1", status: "cancelled" })
        expect(agents[0].blocks.every((block) => !block.pending && !block.streaming && !block.failed)).toBe(true)
        const rendered = render(<ThreadLog blocks={managerBlocks(view.blocks)} />)
        expect(screen.getByText(language === "en" ? "Stopped before collecting delegated results." : "已停止，委派结果未收齐。")).toHaveClass("text-muted-foreground")
        expect(rendered.container).not.toHaveTextContent(/worker_ids|wait_id|generation|running.*true|stopped before collecting worker results/)
        expect(rendered.container.querySelector(".text-destructive, .animate-spin")).toBeNull()
        rendered.unmount()
      }
    })
  }
}

it("preserves the phone's existing unknown-event policy and public notice rendering", () => {
  let view = applyPush(openView({ id: "thread", title: "Work" }), ready([]))
  const unknown = row(20, "future_public_event", { text: "Future event diagnostics" })
  view = applyPush(view, { v: 1, id: "", ok: true, op: OpEvent, event: unknown })
  expect(view.events).toContainEqual(unknown)
  expect(view.blocks.some((block) => block.text === unknown.text)).toBe(false)
  view = applyPush(view, { v: 1, id: "", ok: true, op: OpEvent, event: row(21, "schedule_fired", { text: "A scheduled check ran." }) })
  expect(view.blocks.some((block) => block.kind === "notice" && block.text === "A scheduled check ran.")).toBe(true)
})
