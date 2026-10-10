import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"

import { setLocale } from "@/lib/i18n"
import { applyPush, openView, prependOlder } from "@/lib/session"
import { OpEvent, OpReady, type RemoteEvent, type RemoteResponse } from "@/lib/rpc"
import { ThreadScreen } from "./thread-screen"

it("removes streamed manager paragraphs from the worker roster after older history is loaded", () => {
  setLocale("en")
  const event = (seq: number, kind: string, text: string, extra = {}) => ({
    thread_id: "t1", seq, kind, text, agent_id: "manager",
    created_at: "2026-10-10T00:00:00Z", ...extra,
  } as RemoteEvent)
  const response = (fields: Partial<RemoteResponse>): RemoteResponse => ({ v: 1, id: "", ok: true, ...fields })
  let state = applyPush(openView({ id: "t1", title: "talk" }), {
    ...response({ op: OpReady, seq: 10, events: [event(10, "user_message", "Check the current workspace")], more: true }),
  })
  const handlers = { onBack: vi.fn(), onOlder: vi.fn(), onSend: vi.fn(), onSteer: vi.fn(),
    onStop: vi.fn(), onAnswer: vi.fn(), onAnswerStructured: vi.fn() }
  const props = () => ({ detail: state.detail!, blocks: state.blocks, hasMore: state.hasMore, ...handlers })
  const view = render(<ThreadScreen {...props()} />)
  for (let n = 0; n < 3; n++) {
    // Live deltas are deliberately not stored and have sequence zero.
    for (const next of [
      event(0, "delta", `Manager update ${n}`),
      event(11 + n * 3, "agent_message", `Manager update ${n}`),
      event(12 + n * 3, "tool_call", "read_file({})", { tool_call_id: `read-${n}` }),
      event(13 + n * 3, "tool_result", "Read completed", { tool_call_id: `read-${n}` }),
    ]) {
      state = applyPush(state, response({ op: OpEvent, event: next }))
      view.rerender(<ThreadScreen {...props()} />)
    }
  }
  state = applyPush(state, response({ op: OpEvent, event: event(20, "spawned", "", { agent_id: "worker-1", role: "reader" }) }))
  view.rerender(<ThreadScreen {...props()} />)
  state = prependOlder(state, [event(9, "agent_message", "Older manager answer")], false)
  view.rerender(<ThreadScreen {...props()} />)
  fireEvent.click(screen.getByRole("button", { name: "Agents (1)" }))
  const transcript = screen.getByTestId("transcript")
  expect(screen.getByTestId("agent-roster")).toBeVisible()
  expect(transcript).not.toHaveTextContent("Manager update")
  expect(transcript).not.toHaveTextContent("Older manager answer")
  expect(screen.queryByTestId("work-fold")).not.toBeInTheDocument()
})
