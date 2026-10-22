import { render, screen } from "@testing-library/react"
import { expect, it } from "vitest"
import { AgentTranscript } from "./agent-transcript"
import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import { useApp } from "@/store/app"

it.each(["pending", "recovered", "none"])("shows the durable reason and %s recovery in agent detail without a failure banner", (recovery) => {
  useApp.setState({ locale: "en" })
  const state = reduceEvents(emptyTranscript(), [{ thread_id: "th", turn_id: "turn", agent_id: "worker", kind: recovery === "recovered" ? "generation_recovered" : "generation_interrupted", seq: 2, text: JSON.stringify({ reason: "output_limit", detail: "max tokens", recovery }), created_at: "" }])
  render(<AgentTranscript agent={state.agents.worker} />)
  expect(screen.getByTestId("generation-notice")).toHaveTextContent(/output budget/)
  expect(screen.getByTestId("generation-notice")).toHaveTextContent(recovery === "pending" ? /pending/ : recovery === "recovered" ? /resumed/ : /not scheduled/)
  expect(screen.getByTestId("generation-notice")).not.toHaveClass("text-destructive")
})
