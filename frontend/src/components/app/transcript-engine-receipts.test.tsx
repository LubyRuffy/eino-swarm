import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"
import { TraceTab } from "./trace-tab"
import { Transcript } from "./transcript"

const RAW_ARGS = '{"sql":"SELECT id FROM messages WHERE content LIKE ?","params":["%needle%"]}'

function event(seq: number, kind: string, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return {
    seq, thread_id: "thread", turn_id: "turn", kind, text: "",
    agent_id: "manager", created_at: new Date(Date.UTC(2026, 0, 1, 0, 0, seq)).toISOString(), ...extra,
  }
}

// A history read records its raw SQL before the reader runs. That receipt is
// engine evidence: shown in chat it read as a stray card of tool arguments
// between the work fold and the answer, duplicating the tool row itself.
const events: SwarmEvent[] = [
  event(1, "user_message", { text: "map the running services" }),
  event(2, "history_report_requested"),
  event(3, "tool_call", { text: `query_history(${RAW_ARGS})`, tool_call_id: "read" }),
  event(4, "history_read_started", { agent_id: "", text: RAW_ARGS, tool_call_id: "read" }),
  event(5, "tool_result", { text: '{"ok":true}', tool_call_id: "read" }),
  event(6, "history_registry_inherited", { agent_id: "" }),
  event(7, "agent_message", { text: "the answer" }),
  event(8, "done", { text: "the answer" }),
]

describe("engine history receipts", () => {
  it.each(["user", "developer"] as const)("never become chat rows (%s mode)", (mode) => {
    useApp.setState({ transcriptMode: mode })
    const state = reduceEvents(emptyTranscript(), events)
    const { container } = render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("the answer")).toBeVisible()
    expect(container.textContent).not.toContain("history_read_started")
    expect(container.textContent).not.toContain("history_report_requested")
    expect(container.textContent).not.toContain("history_registry_inherited")
    expect(screen.queryAllByTestId("memory-notice")).toHaveLength(0)
    expect(state.agentOrder).toEqual(["manager"])
  })

  it("stay in the Trace Full log under their stored kind", () => {
    const state = reduceEvents(emptyTranscript(), events)
    render(<TraceTab turns={[]} transcript={state} />)
    fireEvent.click(screen.getByTestId("trace-log-toggle"))
    const log = screen.getByTestId("trace-log")
    expect(log).toHaveTextContent("history_read_started")
    expect(log).toHaveTextContent("history_report_requested")
    expect(log).toHaveTextContent("history_registry_inherited")
    expect(log).toHaveTextContent("%needle%")
  })
})
