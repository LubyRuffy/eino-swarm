import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, expect, it, vi } from "vitest"
import { setLocale } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { OpGraph, type RemoteResponse } from "@/lib/rpc"
import { GRAPH_POLL_RUNNING_MS } from "./work-records"
import { ThreadScreen } from "./thread-screen"

// jsdom has no canvas; the board's drawing is covered by the desktop suite.
vi.mock("../../../frontend/src/components/app/graph/graph-view", () => ({
  GraphView: ({ nodes }: { nodes: { label: string }[] }) => <ul aria-label="drawn graph">{nodes.map((node) => <li key={node.label}>{node.label}</li>)}</ul>,
}))

afterEach(() => { cleanup(); vi.useRealTimers() })

const subject = { id: "s1", kind: "org", name: "Example Org", attrs: {}, status: "confirmed", version: 1, updated_at: "2026-11-24T00:00:00Z" }
const reply = (graph: NonNullable<RemoteResponse["graph"]>): RemoteResponse => ({ v: 1, id: "", ok: true, data_generation: 1, graph })

function setup(rpc: ReturnType<typeof vi.fn<RemoteLink["rpc"]>>) {
  setLocale("en")
  const fileLink: RemoteLink = { rpc, alive: () => true, path: "relay", close: vi.fn(), announceDevice: vi.fn() }
  const props = {
    detail: { id: "thread", title: "Conversation" }, blocks: [], fileLink, panelOwnerKey: "pc-1:1",
    onBack: vi.fn(), onSend: vi.fn(), onStop: vi.fn(), onAnswer: vi.fn(), onAnswerStructured: vi.fn(),
  }
  return { props, view: render(<ThreadScreen {...props} workRecordsGeneration={1} />) }
}

it("hides work records and discards a late PC reply after the setting is turned off", async () => {
  let finishRead!: (value: RemoteResponse) => void
  let requestSignal: AbortSignal | undefined
  const rpc = vi.fn<RemoteLink["rpc"]>(async (request, options) => {
    if (request.op !== OpGraph) return { v: 1, id: "", ok: false, code: "unknown_op" }
    requestSignal = options?.signal
    return new Promise((resolve) => { finishRead = resolve })
  })
  const { props, view } = setup(rpc)
  view.rerender(<ThreadScreen {...props} />)
  expect(screen.queryByRole("button", { name: "Work records" })).toBeNull()
  view.rerender(<ThreadScreen {...props} workRecordsGeneration={1} />)
  expect(rpc.mock.calls.some(([request]) => request.op === OpGraph)).toBe(false)
  fireEvent.click(screen.getByRole("button", { name: "Work records" }))
  expect(await screen.findByRole("heading", { name: "Work records" })).toBeVisible()
  expect(requestSignal?.aborted).toBe(false)
  view.rerender(<ThreadScreen {...props} />)
  expect(requestSignal?.aborted).toBe(true)
  await act(async () => finishRead(reply({ revision: 1 })))
  expect(screen.queryByRole("heading", { name: "Work records" })).toBeNull()
  expect(screen.getByRole("heading", { name: "Conversation" })).toBeVisible()
})

// Polling is how the phone follows a running task; it must send the revision it
// holds so an idle graph costs one small frame, and keep the picture on a miss.
it("polls with the held revision and keeps the picture when a poll fails", async () => {
  vi.useFakeTimers()
  const seen: number[] = []
  let mode: "full" | "unchanged" | "fail" = "full"
  const rpc = vi.fn<RemoteLink["rpc"]>(async (request) => {
    if (request.op !== OpGraph) return { v: 1, id: "", ok: false, code: "unknown_op" }
    seen.push(request.since ?? -1)
    if (mode === "fail") throw new Error("offline")
    if (mode === "unchanged") return reply({ revision: 4, unchanged: true })
    return reply({ revision: 4, subjects: [subject] as never, total: 1 })
  })
  setup(rpc)
  fireEvent.click(screen.getByRole("button", { name: "Work records" }))
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  expect(screen.getByRole("list", { name: "drawn graph" })).toHaveTextContent("Example Org")
  mode = "unchanged"
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  mode = "fail"
  await act(async () => { await vi.advanceTimersByTimeAsync(20_000) })
  expect(screen.getByRole("list", { name: "drawn graph" })).toHaveTextContent("Example Org")
  expect(screen.queryByRole("alert")).toBeNull()
  expect(seen[0]).toBe(0)
  expect(seen.slice(1).every((since) => since === 4)).toBe(true)
  expect(GRAPH_POLL_RUNNING_MS).toBeGreaterThan(0)
})

it("says so, and stops polling, when the PC refuses a graph too large for one frame", async () => {
  vi.useFakeTimers()
  const rpc = vi.fn<RemoteLink["rpc"]>(async (request) => ({ v: 1, id: "", ok: false, code: request.op === OpGraph ? "graph_too_large" : "unknown_op" }))
  setup(rpc)
  fireEvent.click(screen.getByRole("button", { name: "Work records" }))
  await act(async () => { await vi.advanceTimersByTimeAsync(0) })
  expect(screen.getByRole("alert")).toHaveTextContent("too large")
  const reads = () => rpc.mock.calls.filter(([request]) => request.op === OpGraph).length
  expect(reads()).toBe(1)
  await act(async () => { await vi.advanceTimersByTimeAsync(60_000) })
  expect(reads()).toBe(1)
})
