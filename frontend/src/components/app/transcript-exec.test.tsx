import { fireEvent, render, screen } from "@testing-library/react"
import { beforeEach } from "vitest"

import { Transcript } from "./transcript"
import type { AgentState } from "@/lib/transcript"
import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import { useApp } from "@/store/app"

function agent(partial: Partial<AgentState> & { id: string }): AgentState {
  return {
    role: partial.id,
    status: "running",
    activity: "",
    blocks: [],
    ...partial,
  }
}

describe("Transcript exec rows", () => {
  beforeEach(() => {
    // These tests pin the per-row exec chrome. User mode folds it away.
    useApp.setState({ transcriptMode: "developer" })
  })
  it("hides the full command until the row is opened, then wraps every character", () => {
    const command = "cd /tmp/workspace/pkg && for d in alpha beta; do echo $d; done"
    render(
      <Transcript
        state={{
          ...emptyTranscript(),
          agentOrder: ["manager"],
          agents: {
            manager: agent({
              id: "manager",
              role: "manager",
              status: "done",
              blocks: [
                {
                  id: "tool-1",
                  kind: "tool",
                  agentId: "manager",
                  text: "exec",
                  tool: {
                    callId: "c1",
                    name: "exec",
                    args: JSON.stringify({ command }),
                    result: JSON.stringify({
                      exit_code: 0,
                      stdout: "alpha\n",
                      stderr: "",
                      failed: false,
                    }),
                    pending: false,
                  },
                  turnId: "t1",
                  seq: 1,
                  at: new Date().toISOString(),
                },
              ],
            }),
          },
        }}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    expect(screen.queryByTestId("shell-command")).not.toBeInTheDocument()
    expect(screen.getByTestId("shell-command-preview")).toHaveTextContent(command)
    expect(screen.getByTestId("shell-command-preview").className).toMatch(/\btruncate\b/)
    fireEvent.click(screen.getByRole("button", { name: /exec/ }))
    const cmd = screen.getByTestId("shell-command")
    expect(cmd.textContent).toContain(command)
    expect(cmd.className).toMatch(/whitespace-pre-wrap/)
    expect(cmd.className).not.toMatch(/\btruncate\b/)
  })

  it("puts a refused memory write's reason on the collapsed row", () => {
    render(
      <Transcript
        state={{
          ...emptyTranscript(),
          agentOrder: ["manager"],
          agents: {
            manager: agent({
              id: "manager",
              role: "manager",
              status: "done",
              blocks: [
                {
                  id: "tool-1",
                  kind: "tool",
                  agentId: "manager",
                  text: "memory",
                  tool: {
                    callId: "c1",
                    name: "memory",
                    args: JSON.stringify({ action: "replace", content: "a longer status" }),
                    result: JSON.stringify({
                      success: false,
                      error:
                        "memory is at 2200/2200 characters; this write exceeds the limit by 40. Do not retry the same write.",
                      current_entries: ["the one that is already there"],
                    }),
                    pending: false,
                  },
                  turnId: "t1",
                  seq: 1,
                  at: new Date().toISOString(),
                },
              ],
            }),
          },
        }}
        loaded
        onSelectAgent={() => {}}
      />,
    )
    const row = screen.getByRole("button", { name: /memory/ })
    expect(row).toHaveTextContent("Do not retry")
    expect(row).toHaveTextContent("2200/2200")
    expect(screen.queryByRole("alert")).not.toBeInTheDocument()
  })

  it("stops spinning an exec that was still open when the turn was interrupted", () => {
    const at = "2024-01-01T00:00:00Z"
    const state = reduceEvents(emptyTranscript(), [
      {
        thread_id: "th",
        turn_id: "t1",
        seq: 1,
        kind: "user_message",
        agent_id: "manager",
        text: "go",
        created_at: at,
      },
      {
        thread_id: "th",
        turn_id: "t1",
        seq: 2,
        kind: "tool_call",
        agent_id: "manager",
        text: 'exec({"command":"sleep 30"})',
        tool_call_id: "c1",
        created_at: at,
      },
      {
        thread_id: "th",
        turn_id: "t1",
        seq: 3,
        kind: "error",
        agent_id: "manager",
        err: "interrupted",
        created_at: at,
      },
    ])
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(document.querySelector(".animate-spin")).toBeNull()
    expect(screen.queryByText("running…")).not.toBeInTheDocument()
    expect(screen.getByText("interrupted")).toBeInTheDocument()
  })

  it("stops spinning an exec that was still open when the crashed turn is resumed", () => {
    const at = "2024-01-01T00:00:00Z"
    const state = reduceEvents(emptyTranscript(), [
      {
        thread_id: "th",
        turn_id: "t1",
        seq: 1,
        kind: "user_message",
        agent_id: "manager",
        text: "go",
        created_at: at,
      },
      {
        thread_id: "th",
        turn_id: "t1",
        seq: 2,
        kind: "tool_call",
        agent_id: "manager",
        text: 'exec({"command":"sleep 30"})',
        tool_call_id: "c1",
        created_at: at,
      },
      {
        thread_id: "th",
        turn_id: "t1",
        seq: 3,
        kind: "resumed",
        agent_id: "manager",
        text: "the previous run was interrupted; continuing",
        created_at: at,
      },
    ])
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(document.querySelector(".animate-spin")).toBeNull()
    expect(screen.queryByText("running…")).not.toBeInTheDocument()
    const row = screen.getByRole("button", { name: /exec/ })
    expect(row).toHaveAttribute("aria-invalid", "true")
    fireEvent.click(row)
    expect(screen.getByText("the previous process stopped")).toBeInTheDocument()
  })
})

