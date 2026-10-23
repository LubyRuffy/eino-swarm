import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it } from "vitest"
import { fake } from "@/store/app-test-fixture"
import { useApp } from "@/store/app"
import type { SwarmEvent } from "@/lib/types"
import { TraceTab } from "./trace-tab"

it("retains the actual ask recovery identity in Full log while hiding private checkpoints", async () => {
  fake.logEvents = []
  await useApp.getState().boot()
  await useApp.getState().openThread("th_old")
  const base = { thread_id: "th_old", turn_id: "turn", agent_id: "manager", created_at: "2026-10-23T12:00:00Z" }
  const args = JSON.stringify({ questions: [{ id: "path", prompt: "Choose a path", options: [{ id: "one", label: "First" }, { id: "two", label: "Second" }] }] })
  const events: SwarmEvent[] = [
    { ...base, seq: 1, kind: "user_message", text: "Inspect the workspace" },
    { ...base, seq: 2, kind: "tool_call", tool_call_id: "ask-original", text: `ask_user(${args})` },
    { ...base, seq: 3, kind: "engine_restart_checkpoint", text: '{"manager_history":["private seed"],"pause":"ask"}' },
    { ...base, seq: 4, kind: "ask_recovered", tool_call_id: "ask-original", text: args },
  ]
  for (const event of events) fake.onEvent?.({ ...event })
  const transcript = useApp.getState().transcript
  expect(transcript.agents.manager.blocks.filter((b) => b.kind === "question")).toHaveLength(1)
  render(<TraceTab turns={[]} transcript={transcript} />)
  fireEvent.click(screen.getByRole("button", { name: /Full log/i }))
  const log = screen.getByTestId("trace-log")
  expect(log).toHaveTextContent("ask_recovered")
  expect(log).toHaveTextContent("ask-original")
  expect(log).toHaveTextContent('"seq": 4')
  expect(log).not.toHaveTextContent("private seed")
  expect(log).not.toHaveTextContent("engine_restart_checkpoint")
})
