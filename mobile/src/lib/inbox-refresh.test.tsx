import { act, renderHook } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { readInbox } from "./completion-read"
import { emptyInbox, inboxThreads, reduceInbox } from "./inbox-window"
import { compareAttention, readAttentionWindow, reconcileInbox, useInboxRefresh } from "./inbox-refresh"
import type { RemoteLink } from "./link"
import type { RemoteResponse, ThreadView } from "./rpc"

const row = (id: string, fraction: string, seq = 12): ThreadView => ({ id, title: id, running: false,
  last_active_at: `2026-01-01T00:00:00.${fraction}Z`, unread: true, completed_seq: seq })
const a = row("a", "004"), b = row("b", "003"), c = row("c", "002"), d = row("d", "001")
const page = (rows: ThreadView[], more = false, cursor = ""): RemoteResponse => ({ v: 1, id: "r", ok: true,
  attention: rows, attention_more: more, attention_cursor: cursor })
const target = (...pages: RemoteResponse[]) => ({ rpc: vi.fn(async () => pages.shift()!), alive: () => true }) as unknown as RemoteLink
const loaded = () => reduceInbox(reduceInbox(emptyInbox(), { ...page([a, b], true, "opaque:head"),
  threads: [{ ...a, id: "ordinary", unread: false }], more: true, next: "ordinary-next" }, "replace"),
page([c, d], true, "opaque:tail"), "append", "attention")

describe("refreshing the expanded completion window", () => {
  it("reconciles a remotely read tail after an unchanged first page without changing ordinary pagination", async () => {
    const prior = loaded(), link = target(page([c]))
    const first = { ...page([a, b], true, "opaque:head"), threads: prior.head, more: true, next: "ordinary-next" }
    const snapshot = await readAttentionWindow(link, first, prior)
    const next = reconcileInbox(prior, first, snapshot!)
    expect(next.attention.map((r) => r.id)).toEqual(["a", "b", "c"])
    expect(link.rpc).toHaveBeenCalledWith({ op: "more", group: "attention", cursor: "opaque:head" })
    expect(inboxThreads(next)).toEqual(inboxThreads(prior))
    expect(next.more).toBe(true)
    expect(next.cursor).toBe("ordinary-next")
    expect(next.groups.find((g) => g.id === "attention")?.more).toBe(false)
  })

  it("reads through the old keyset boundary after new head rows displace the old tail", async () => {
    const newer = row("new", "005"), prior = loaded(), link = target(page([b, c], true, "second"), page([d], true, "third"))
    const first = page([newer, a], true, "first")
    const snapshot = await readAttentionWindow(link, first, prior)
    expect(reconcileInbox(prior, first, snapshot!).attention.map((r) => r.id)).toEqual(["new", "a", "b", "c", "d"])
    expect(link.rpc).toHaveBeenCalledTimes(2)
    expect(snapshot!.group.cursor).toBe("third")
  })

  it("keeps nanosecond precision and crosses a removed boundary rather than stopping in its millisecond", async () => {
    const upper = row("upper", "001900"), boundary = row("boundary", "001100")
    const between = row("between", "0012"), lower = row("lower", "001099999")
    const prior = reduceInbox(reduceInbox(emptyInbox(), page([upper], true, "old-first"), "replace"),
      page([boundary], true, "old-tail"), "append", "attention")
    const link = target(page([between], true, "next"), page([lower], true, "after"))
    const snapshot = await readAttentionWindow(link, page([upper], true, "first"), prior)
    expect(link.rpc).toHaveBeenCalledTimes(2)
    expect(snapshot!.rows.map((r) => r.id)).toEqual(["upper", "between", "lower"])
    expect(compareAttention(between, boundary)).toBeGreaterThan(0)
    expect(compareAttention(row("a", "0011"), boundary)).toBeLessThan(0)
    expect(compareAttention(row("boundary", "001100000"), boundary)).toBe(0)
    expect(compareAttention(row("z", "0011"), boundary)).toBeGreaterThan(0)
    expect(compareAttention(a, row("other", "001"))).toBeGreaterThan(0)
    expect(compareAttention({ ...a, last_active_at: "2025-12-31T23:59:59.999999999Z" }, d)).toBeLessThan(0)
  })

  it("does not fetch extra pages for an unexpanded first page", async () => {
    const prior = reduceInbox(emptyInbox(), page([a, b], true, "head"), "replace"), link = target()
    const snapshot = await readAttentionWindow(link, page([a], true, "fresh"), prior)
    expect(link.rpc).not.toHaveBeenCalled()
    expect(reconcileInbox(prior, page([a], true, "fresh"), snapshot!).attention).toEqual([a])
    expect(snapshot!.group.cursor).toBe("fresh")
  })

  it("reads an expanded window even when the budget-limited first page only has More", async () => {
    const link = target(page([a, b], true, "next"), page([c, d]))
    const snapshot = await readAttentionWindow(link, page([], true, "first"), loaded())
    expect(snapshot!.rows).toEqual([a, b, c, d])
    expect(link.rpc).toHaveBeenNthCalledWith(1, { op: "more", group: "attention", cursor: "first" })
  })

  it("retains a concurrent More extension and its continuation outside the captured boundary", async () => {
    const prior = loaded(), e = row("e", "0009")
    const snapshot = await readAttentionWindow(target(page([c, d], true, "fresh-tail")), page([a, b], true, "head"), prior)
    const current = reduceInbox(prior, page([e], true, "new-tail"), "append", "attention")
    const next = reconcileInbox(current, page([a, b], true, "head"), snapshot!)
    expect(next.attention.map((r) => r.id)).toEqual(["a", "b", "c", "d", "e"])
    expect(next.groups.find((g) => g.id === "attention")?.cursor).toBe("new-tail")
  })

  it("keeps a confirmed read made during refresh but accepts a higher new completion", async () => {
    const prior = loaded()
    const snapshot = await readAttentionWindow(target(page([c, d])), page([a, b], true, "head"), prior)
    const current = readInbox(prior, { id: "d", title: "d", unread: false, completed_seq: 12 })
    expect(reconcileInbox(current, page([a, b], true, "head"), snapshot!).attention.map((r) => r.id)).toEqual(["a", "b", "c"])
    const newer = await readAttentionWindow(target(page([c, { ...d, completed_seq: 14 }])), page([a, b], true, "head"), prior)
    expect(reconcileInbox(current, page([a, b], true, "head"), newer!).attention.at(-1)?.completed_seq).toBe(14)
    expect(emptyInbox().confirmedReads).toEqual({})
    const stillUnread = readInbox(prior, { id: "d", title: "d", unread: true, completed_seq: 14 })
    expect(stillUnread.confirmedReads).toEqual({})
  })

  it.each(["missing", "older"] as const)("retains a higher current completion against a %s request-start snapshot", async (kind) => {
    const prior = loaded()
    const snapshot = await readAttentionWindow(target(page(kind === "missing" ? [c] : [c, d])),
      page([a, b], true, "head"), prior)
    // The old read12 acknowledgement observes a newer unread14 on the host.
    const current = readInbox(prior, { id: "d", title: "d", unread: true, completed_seq: 14 })
    const next = reconcileInbox(current, page([a, b], true, "head"), snapshot!)
    expect(next.attention.find((r) => r.id === "d")).toMatchObject({ completed_seq: 14, unread: true })
  })

  it.each(["during", "before"] as const)("retains a newly verified ordinary-row completion after attention was read %s refresh", async (when) => {
    const full = reduceInbox(loaded(), { threads: [d] }, "append")
    const read = readInbox(full, { id: "d", title: "d", unread: false, completed_seq: 12 })
    const prior = when === "before" ? read : full
    const snapshot = await readAttentionWindow(target(page([c])), page([a, b], true, "head"), prior)
    const current = readInbox(read, { id: "d", title: "d", unread: true, completed_seq: 14 })
    expect(current.attention.some((row) => row.id === "d")).toBe(false)
    expect(current.tail.find((row) => row.id === "d")).toMatchObject({ completed_seq: 14, unread: true })
    expect(reconcileInbox(current, page([a, b], true, "head"), snapshot!).attention.find((r) => r.id === "d"))
      .toMatchObject({ completed_seq: 14, unread: true })
  })

  it("rejects non-advancing pages rather than looping forever", async () => {
    await expect(readAttentionWindow(target(page([a], true, "same")), page([a, b], true, "same"), loaded())).rejects.toThrow("did not advance")
    await expect(readAttentionWindow(target(page([], true, "new")), page([a, b], true, "first"), loaded())).rejects.toThrow("did not advance")
    await expect(readAttentionWindow(target(), page([a, b], true), loaded())).rejects.toThrow("did not advance")
    await expect(readAttentionWindow(target(page([a], true, "changed")), page([a, b], true, "first"), loaded())).rejects.toThrow("did not advance")
    expect(() => compareAttention({ ...a, last_active_at: "invalid" }, d)).toThrow("invalid completion timestamp")
    expect(() => compareAttention({ ...a, last_active_at: "2026-99-99T00:00:00Z" }, d)).toThrow("invalid completion timestamp")
  })
})

describe("inbox refresh requests", () => {
  it("releases a failed request so the next poll can refresh the same host", async () => {
    const rpc = vi.fn().mockRejectedValueOnce(new Error("transport unavailable")).mockResolvedValueOnce(page([a]))
    const target = { rpc } as unknown as RemoteLink
    const link = { current: target }, window = { current: emptyInbox() }, onPage = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useInboxRefresh({ link, window, onPage, onError }))
    await act(async () => { expect(await hook.result.current(target)).toBeNull() })
    expect(onError).toHaveBeenCalledWith(expect.stringContaining("transport unavailable"))
    await act(async () => { expect(await hook.result.current(target)).toMatchObject({ ok: true }) })
    expect(onPage).toHaveBeenCalledTimes(1)
  })

  it("preserves the old window on any continuation failure", async () => {
    const prior = loaded(), link = { current: target(page([a, b], true, "first"), { ...page([]), ok: false, error: "page unavailable" }) }
    const window = { current: prior }, onPage = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useInboxRefresh({ link, window, onPage, onError }))
    let result: RemoteResponse | null | undefined
    await act(async () => { result = await hook.result.current(link.current) })
    expect(result).toBeNull()
    expect(window.current).toBe(prior)
    expect(onPage).not.toHaveBeenCalled()
    expect(onError).toHaveBeenCalledWith(expect.stringContaining("page unavailable"))
  })

  it("does not overlap polls and rejects an old host's first page and continuation", async () => {
    let finish!: (page: RemoteResponse) => void
    const old = { rpc: vi.fn(() => new Promise<RemoteResponse>((resolve) => { finish = resolve })) } as unknown as RemoteLink
    const link = { current: old }, window = { current: loaded() }, onPage = vi.fn(), onError = vi.fn()
    const hook = renderHook(() => useInboxRefresh({ link, window, onPage, onError }))
    let first!: Promise<RemoteResponse | null>
    act(() => { first = hook.result.current(old) })
    expect(await hook.result.current(old)).toBeNull()
    expect(old.rpc).toHaveBeenCalledTimes(1)
    link.current = target(page([a]))
    window.current = emptyInbox()
    await act(async () => { finish(page([a, b], true, "next")); await first })
    expect(onPage).not.toHaveBeenCalled()
    expect(onError).not.toHaveBeenCalled()
    await act(async () => { expect(await hook.result.current(link.current)).toMatchObject({ ok: true }) })
    expect(onPage).toHaveBeenCalledTimes(1)

    link.current = old
    window.current = loaded()
    act(() => { first = hook.result.current(old, page([a, b], true, "next")) })
    link.current = target()
    await act(async () => { finish(page([c, d])); await first })
    expect(onPage).toHaveBeenCalledTimes(1)
    expect(await hook.result.current(old)).toBeNull()
  })
})
