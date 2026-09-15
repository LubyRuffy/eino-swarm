import { beforeEach, describe, expect, it, vi } from "vitest"

import { KINDS, subscribeEvents } from "./stream"
import type { EventKind } from "./types"

/** A stand-in for the browser's EventSource that records what was subscribed
 *  to and can deliver a named event. */
class FakeEventSource {
  static last: FakeEventSource
  static readonly CLOSED = 2
  readyState = 1
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  listeners = new Map<string, (e: MessageEvent) => void>()

  constructor(readonly url: string) {
    FakeEventSource.last = this
  }

  addEventListener(kind: string, fn: (e: MessageEvent) => void) {
    this.listeners.set(kind, fn)
  }

  close() {
    this.closed = true
  }

  deliver(kind: string, data: unknown) {
    this.listeners.get(kind)?.({
      type: kind,
      data: JSON.stringify(data),
    } as MessageEvent)
  }
}

beforeEach(() => {
  vi.stubGlobal("EventSource", FakeEventSource)
})

// The wire contract, kept beside the Go test that freezes the same strings.
// A kind the app renders but does not subscribe to is the worst kind of bug:
// everything is stored, the trace shows it, and the screen never moves.
const wireKinds: EventKind[] = [
  "user_message",
  "agent_message",
  "reasoning",
  "reasoning_delta",
  "delta",
  "turn",
  "spawned",
  "finished",
  "tool_call",
  "tool_result",
  "steer",
  "cleanup",
  "progress",
  "memory_review",
  "done",
  "error",
]

describe("the event stream", () => {
  it("subscribes to every kind the server sends", () => {
    subscribeEvents("th_1", { onEvent: vi.fn() })
    for (const kind of wireKinds) {
      expect(KINDS).toContain(kind)
      expect(FakeEventSource.last.listeners.has(kind)).toBe(true)
    }
  })

  it("delivers a review that lands after the turn has finished", () => {
    const onEvent = vi.fn()
    subscribeEvents("th_1", { onEvent })
    FakeEventSource.last.deliver("done", { kind: "done", seq: 27 })
    FakeEventSource.last.deliver("memory_review", {
      kind: "memory_review",
      seq: 28,
      text: '{"changed":true}',
    })
    expect(onEvent.mock.calls.map(([ev]) => ev.kind)).toEqual([
      "done",
      "memory_review",
    ])
  })

  // The event name is the authority: a payload whose kind disagrees with the
  // name it arrived under would be folded in as the wrong thing.
  it("trusts the event name over the payload", () => {
    const onEvent = vi.fn()
    subscribeEvents("th_1", { onEvent })
    FakeEventSource.last.deliver("cleanup", { kind: "done", seq: 5 })
    expect(onEvent.mock.calls[0][0].kind).toBe("cleanup")
  })

  it("resumes from where the app left off", () => {
    subscribeEvents("th_1", { onEvent: vi.fn() }, 12)
    expect(FakeEventSource.last.url).toBe("/api/threads/th_1/events?since=12")
  })

  it("closes the connection when the caller unsubscribes", () => {
    const onClose = vi.fn()
    const stop = subscribeEvents("th_1", { onEvent: vi.fn(), onClose })
    stop()
    expect(FakeEventSource.last.closed).toBe(true)
    expect(onClose).toHaveBeenCalledWith("closed")
  })

  // A connection that dropped on its own is retried by the browser; only a
  // connection that is closed for good is worth telling the UI about.
  it("reports only a terminal failure, and not after a deliberate close", () => {
    const onClose = vi.fn()
    const stop = subscribeEvents("th_1", { onEvent: vi.fn(), onClose })
    FakeEventSource.last.readyState = 1
    FakeEventSource.last.onerror?.()
    expect(onClose).not.toHaveBeenCalled()

    FakeEventSource.last.readyState = FakeEventSource.CLOSED
    FakeEventSource.last.onerror?.()
    expect(onClose).toHaveBeenCalledWith("error")

    onClose.mockClear()
    stop()
    FakeEventSource.last.onerror?.()
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
