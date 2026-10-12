import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { openSaved, type DeviceLink } from "@/lib/client"
import { saveLastThreadId, saveLink } from "@/lib/store"
import type { RemoteRequest, RemoteResponse } from "@/lib/rpc"

vi.mock("@/lib/client", async (original) => ({
  ...await original<typeof import("@/lib/client")>(), openSaved: vi.fn(),
}))
import { App } from "./app"

function fixture(opts: { last?: boolean; openFails?: boolean; watchFails?: boolean; readFails?: boolean; readFailsOnce?: boolean; emptyAttentionPage?: boolean; moreFails?: boolean } = {}) {
  saveLink({ hubURL: "http://127.0.0.1:9", ticket: "t", hostPub: "p", sessionID: "s", fingerprint: "fp" })
  if (opts.last) saveLastThreadId("done-thread")
  let unread = true
  let readFailures = opts.readFailsOnce ? 1 : 0
  const calls: Partial<RemoteRequest>[] = []
  const row = () => ({ id: "done-thread", title: "Completed task", running: false,
    last_active_at: "2026-01-01T00:00:00Z", unread, completed_seq: 12 })
  const response = (part: object): RemoteResponse => ({ v: 1, id: "r", ok: true, ...part })
  const link = {
    path: "relay", alive: () => true, close: vi.fn(), announceDevice: async () => undefined,
    rpc: async (request: Partial<RemoteRequest>) => {
      calls.push(request)
      if (request.op === "list") return opts.emptyAttentionPage
        ? response({ threads: [{ ...row(), id: "ordinary", title: "Ordinary conversation", unread: false }], running: [],
          attention: [], attention_more: unread, attention_cursor: unread ? "opaque:first" : "" })
        : response({ threads: [row()], running: [], attention: unread ? [row()] : [] })
      if (request.op === "more") return opts.moreFails ? response({ ok: false, error: "attention unavailable" })
        : response({ attention: unread ? [row()] : [], attention_more: false, attention_cursor: "" })
      if (request.op === "open") return opts.openFails ? response({ ok: false, error: "could not open" }) : response({ detail: { ...row(), running: undefined } })
      if (request.op === "watch") return opts.watchFails ? response({ ok: false, error: "watch unavailable" }) : response({ op: "ready", thread_id: "done-thread", seq: 12,
        events: [{ thread_id: "done-thread", seq: 12, kind: "done", text: "", created_at: "2026-01-01T00:00:00Z" }] })
      if (request.op === "read") {
        if (opts.readFails || readFailures-- > 0) return response({ ok: false, error: "read unavailable" })
        unread = false
        return response({ detail: { ...row(), running: undefined } })
      }
      return response({})
    },
  } as unknown as DeviceLink
  vi.mocked(openSaved).mockResolvedValue(link)
  return { link, calls, readCalls: () => calls.filter((r) => r.op === "read") }
}

afterEach(() => { cleanup(); vi.mocked(openSaved).mockReset() })

describe("loaded completion window refresh", () => {
  it("removes a completion read on desktop even when page one is unchanged", async () => {
    const host = fixture()
    let readElsewhere = false
    const completed = (id: string, day: number) => ({ id, title: `Task ${id}`, running: false,
      last_active_at: `2026-01-0${day}T00:00:00Z`, unread: true, completed_seq: 12 })
    const rpc = host.link.rpc
    host.link.rpc = async (request, opts) => {
      if (request.op === "list") return { v: 1, id: "r", ok: true, threads: [], running: [],
        attention: [completed("a", 4), completed("b", 3)], attention_more: true, attention_cursor: "opaque-head" }
      if (request.op === "more") return { v: 1, id: "r", ok: true,
        attention: readElsewhere ? [completed("c", 2)] : [completed("c", 2), completed("d", 1)], attention_more: false }
      return rpc(request, opts)
    }
    render(<App />)
    const section = await screen.findByTestId("completion-attention")
    fireEvent.click(within(section).getByRole("button", { name: "More" }))
    await within(section).findByRole("button", { name: "Open Task d" })
    readElsewhere = true
    await waitFor(() => expect(within(section).queryByRole("button", { name: "Open Task d" })).not.toBeInTheDocument(), { timeout: 3500 })
    expect(within(section).getByRole("button", { name: "Open Task c" })).toBeInTheDocument()
  })
})

describe("completed task attention", () => {
  it("keeps completed unread tasks at the top with a static state distinct from running", async () => {
    fixture()
    render(<App />)
    const section = await screen.findByTestId("completion-attention")
    expect(within(section).getByText("Completed unread")).toBeInTheDocument()
    const state = within(section).getByTestId("row-state")
    expect(state).toHaveAttribute("data-state", "unread")
    expect(state.querySelector(".animate-pulse, .motion-safe\\:animate-pulse")).toBeNull()
    expect(screen.queryByText("In progress")).not.toBeInTheDocument()
  })

  it("acknowledges the completion watermark only after explicit opening renders the detail", async () => {
    const host = fixture()
    render(<App />)
    fireEvent.click(within(await screen.findByTestId("completion-attention")).getByRole("button", { name: "Open Completed task" }))
    await screen.findByTestId("transcript")
    await waitFor(() => expect(host.readCalls()).toEqual([{ op: "read", thread_id: "done-thread", completed_seq: 12 }]))
    fireEvent.click(screen.getByRole("button", { name: "Back" }))
    await waitFor(() => expect(screen.queryByTestId("completion-attention")).not.toBeInTheDocument())
  })

  it("does not mark a completion read when reopening the last thread automatically", async () => {
    const host = fixture({ last: true })
    render(<App />)
    await screen.findByTestId("transcript")
    await act(async () => undefined)
    expect(host.readCalls()).toEqual([])
    fireEvent.click(screen.getByRole("button", { name: "Back" }))
    expect(await screen.findByTestId("completion-attention")).toBeInTheDocument()
  })

  it("retains unread state when explicit navigation or its read acknowledgement fails", async () => {
    const host = fixture({ readFails: true })
    render(<App />)
    fireEvent.click(within(await screen.findByTestId("completion-attention")).getByRole("button", { name: "Open Completed task" }))
    await screen.findByTestId("transcript")
    await waitFor(() => expect(host.readCalls()).toHaveLength(1))
    expect(await screen.findByRole("alert")).toHaveTextContent("read unavailable")
    fireEvent.click(screen.getByRole("button", { name: "Back" }))
    expect(await screen.findByTestId("completion-attention")).toBeInTheDocument()
  })
  it("clears a failed read alert after a later explicit visit is acknowledged", async () => {
    const host = fixture({ readFailsOnce: true })
    render(<App />)
    const open = async () => {
      const button = within(await screen.findByTestId("completion-attention")).getByRole("button", { name: "Open Completed task" })
      await act(async () => { fireEvent.click(button) })
    }
    await open()
    await screen.findByTestId("transcript")
    expect(await screen.findByRole("alert")).toHaveTextContent("read unavailable")
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Back" })) })
    await open()
    await screen.findByTestId("transcript")
    await waitFor(() => expect(host.readCalls()).toHaveLength(2))
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument())
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Back" })) })
    await waitFor(() => expect(screen.queryByTestId("completion-attention")).not.toBeInTheDocument())
  })

  it.each(["openFails", "watchFails"] as const)("does not acknowledge a failed %s navigation", async (failure) => {
    const host = fixture({ [failure]: true })
    render(<App />)
    fireEvent.click(within(await screen.findByTestId("completion-attention")).getByRole("button", { name: "Open Completed task" }))
    await screen.findByRole("alert")
    await act(async () => undefined)
    expect(host.readCalls()).toEqual([])
  })

  it("acknowledges only a new visible primary completion, not replay, workers or background events", async () => {
    const host = fixture({ last: true })
    render(<App />)
    await screen.findByTestId("transcript")
    const push = (seq: number, agentID = "manager", kind = "done") => host.link.onPush?.({ v: 1, id: "", ok: true, op: "event", thread_id: "done-thread",
      event: { thread_id: "done-thread", seq, kind, agent_id: agentID, text: "", created_at: "" } })
    await act(async () => push(13, "worker", "error"))
    expect(host.readCalls()).toEqual([])
    const hidden = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden")
    await act(async () => push(14))
    expect(host.readCalls()).toEqual([])
    hidden.mockRestore()
    await act(async () => push(15))
    await waitFor(() => expect(host.readCalls()).toEqual([{ op: "read", thread_id: "done-thread", completed_seq: 15 }]))
    await act(async () => push(15))
    expect(host.readCalls()).toHaveLength(1)
  })
  it("offers completion More even when the initial frame only has an opaque cursor", async () => {
    const host = fixture({ emptyAttentionPage: true })
    render(<App />)
    const section = await screen.findByTestId("completion-attention")
    expect(within(section).queryByRole("button", { name: "Open Completed task" })).not.toBeInTheDocument()
    fireEvent.click(within(section).getByRole("button", { name: "More" }))
    await within(section).findByRole("button", { name: "Open Completed task" })
    expect(host.calls.filter((r) => r.op === "more")).toEqual([{ op: "more", group: "attention", cursor: "opaque:first" }])
    expect(screen.getByRole("button", { name: "Open Ordinary conversation" })).toBeInTheDocument()
    expect(within(section).queryByRole("button", { name: "More" })).not.toBeInTheDocument()
  })

  it("keeps the completion page entry and ordinary rows when More fails", async () => {
    fixture({ emptyAttentionPage: true, moreFails: true })
    render(<App />)
    const section = await screen.findByTestId("completion-attention")
    fireEvent.click(within(section).getByRole("button", { name: "More" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("attention unavailable")
    expect(within(section).getByRole("button", { name: "More" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "Open Ordinary conversation" })).toBeInTheDocument()
  })

})
