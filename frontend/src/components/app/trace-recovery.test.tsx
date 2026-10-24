import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import { TraceTab } from "./trace-tab"

it.each(["desktop_recovery", "engine_restore"])("shows a %s recovery identity in Full log without changing the completed task", (kind) => {
  const base = { thread_id: "th_recovery", turn_id: "tn_recovery", agent_id: "manager", created_at: "2026-10-22T00:00:00Z" }
  const transcript = reduceEvents(emptyTranscript(), [
    { ...base, seq: 1, kind: "user_message", text: "task" },
    { ...base, seq: 2, kind: "done", text: "finished" },
    { ...base, seq: 3, kind, agent_id: "", text: '{"session_id":"session-original","phase":"recovered"}' },
  ])
  render(<TraceTab turns={[]} transcript={transcript} />)
  expect(screen.getByTestId("trace-status")).toHaveTextContent("done")
  fireEvent.click(screen.getByTestId("trace-log-toggle"))
  expect(screen.getByTestId("trace-log")).toHaveTextContent(kind)
  expect(screen.getByTestId("trace-log")).toHaveTextContent("session-original")
  expect(screen.getByTestId("trace-log")).toHaveTextContent('"seq": 3')
})
