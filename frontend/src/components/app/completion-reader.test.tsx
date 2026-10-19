import { act, render, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Thread } from "@/lib/types"
import { emptyTranscript, reduceEvent, rewindTranscript } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"
import { CompletionReader, applyCompletionRead } from "./completion-reader"
import { mergeThreadList } from "@/lib/thread-title"
const read = vi.hoisted(() => vi.fn())
vi.mock("@/lib/api", () => ({ api: { readThread: read }, ApiError: Error }))
const row = { id: "one", completed_seq: 12, unread: true, running: false } as Thread
const event = (kind: string, seq: number, more: Partial<SwarmEvent> = {}) =>
  ({ kind, seq, turn_id: "turn-one", agent_id: "manager", text: "Observed result", ...more }) as SwarmEvent
const displayed = () => [event("user_message", 10), event("agent_message", 11), event("done", 12)]
  .reduce((state, ev) => reduceEvent(state, ev), emptyTranscript())
beforeEach(() => {
  read.mockReset().mockResolvedValue({ thread: { ...row, unread: false } })
  Object.defineProperty(document, "visibilityState", { configurable: true, value: "visible" })
  useApp.setState({ activeId: row.id, loaded: true, threads: [row],
    transcript: displayed(), error: undefined, completionVisit: 0 })
})
describe("reading a displayed completion", () => {
  it("acks only the completion present in the visible loaded transcript", async () => {
    render(<CompletionReader />)
    await waitFor(() => expect(read).toHaveBeenCalledWith("one", 12))
    await waitFor(() => expect(useApp.getState().threads[0].unread).toBe(false))
  })
  it("acks a displayed stopped turn without calling it successful", async () => {
    const transcript = [event("user_message", 10), event("user_stop_requested", 11), event("error", 12, { err: "interrupted" })]
      .reduce((state, ev) => reduceEvent(state, ev), emptyTranscript())
    expect(transcript.turns[0].status).toBe("cancelled")
    useApp.setState({ transcript })
    render(<CompletionReader />)
    await waitFor(() => expect(read).toHaveBeenCalledWith("one", 12))
    await waitFor(() => expect(useApp.getState().threads[0].unread).toBe(false))
  })
  it.each(["loading", "not displayed", "hidden"])("keeps attention while %s", async (state) => {
    if (state === "loading") useApp.setState({ loaded: false })
    if (state === "not displayed") useApp.setState({ transcript: emptyTranscript() })
    if (state === "hidden") Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true })
    render(<CompletionReader />)
    await act(async () => {})
    expect(read).not.toHaveBeenCalled()
    expect(useApp.getState().threads[0].unread).toBe(true)
  })
  // A resume cursor can advance from worker/roster history or survive rewind;
  // neither proves that this completion's primary result reached the screen.
  it.each(["worker only", "roster only", "rewound", "quiet"])("does not consume a completion from %s history", async (source) => {
    let transcript = displayed()
    if (source === "worker only") transcript = reduceEvent(emptyTranscript(), event("finished", 20, { agent_id: "worker" }))
    if (source === "roster only") {
      transcript = reduceEvent(emptyTranscript(), event("user_message", 10))
      transcript = reduceEvent(transcript, event("done", 12), "roster")
    }
    if (source === "rewound") transcript = rewindTranscript(transcript, 11)
    if (source === "quiet") transcript = { ...transcript, turns: transcript.turns.map((turn) => ({ ...turn, quiet: true })) }
    useApp.setState({ transcript })
    render(<CompletionReader />)
    await act(async () => {})
    expect(read).not.toHaveBeenCalled()
    expect(useApp.getState().threads[0].unread).toBe(true)
  })
  it("keeps the badge and exposes a failed ACK without infinite retries", async () => {
    read.mockRejectedValue(new Error("read failed"))
    render(<CompletionReader />)
    await waitFor(() => expect(useApp.getState().error).toBe("read failed"))
    expect(useApp.getState().threads[0].unread).toBe(true)
    expect(read).toHaveBeenCalledTimes(1)
    read.mockResolvedValue({ thread: { ...row, unread: false } })
    act(() => useApp.setState({ completionVisit: 1 }))
    await waitFor(() => expect(useApp.getState().threads[0].unread).toBe(false))
    expect(read).toHaveBeenCalledTimes(2)
  })
  it("preserves a newer completion and live status when an old ACK returns", () => {
    const newer = { ...row, completed_seq: 20, running: true }
    expect(applyCompletionRead([newer], { ...row, unread: false })).toEqual([newer])
    const readRow = applyCompletionRead([{ ...row, title: "current", running: true }], { ...row, unread: false })[0]
    expect(readRow).toMatchObject({ unread: false, title: "current", running: true })
  })
  it("does not relight a result when an older ACK returns its earlier unread snapshot last", async () => {
    const replies = new Map<number, (value: { thread: Thread }) => void>()
    read.mockImplementation((_id: string, seq: number) => new Promise((resolve) => replies.set(seq, resolve)))
    render(<CompletionReader />)
    await waitFor(() => expect(replies.has(12)).toBe(true))
    const newer = { ...row, completed_seq: 14 }
    const transcript = [event("user_message", 13, { turn_id: "new-turn" }), event("done", 14, { turn_id: "new-turn" })]
      .reduce((state, ev) => reduceEvent(state, ev), emptyTranscript())
    act(() => useApp.setState({ threads: [newer], transcript }))
    await waitFor(() => expect(replies.has(14)).toBe(true))
    await act(async () => replies.get(14)!({ thread: { ...newer, unread: false } }))
    expect(useApp.getState().threads[0].unread).toBe(false)
    // The old request may have obtained seq14 before its own read completed.
    await act(async () => replies.get(12)!({ thread: { ...newer, unread: true } }))
    expect(useApp.getState().threads[0].unread).toBe(false)
  })
  it("ignores a stale unread listing without hiding a newer result or rewind", () => {
    const local = [{ ...row, unread: false }]
    expect(mergeThreadList(local, [row])[0].unread).toBe(false)
    expect(mergeThreadList(local, [{ ...row, completed_seq: 20 }])[0].unread).toBe(true)
    expect(mergeThreadList(local, [{ ...row, completed_seq: 0, unread: false }])[0].completed_seq).toBe(0)
  })
})
