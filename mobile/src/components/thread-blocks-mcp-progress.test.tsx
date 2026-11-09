import { fireEvent, render, screen, within } from "@testing-library/react"
import { expect, it } from "vitest"

import { setLocale } from "@/lib/i18n"
import { OpEvent, OpReady, type RemoteEvent } from "@/lib/rpc"
import { applyPush, openView } from "@/lib/session"
import { phoneAgents } from "@/lib/transcript"

import { ThreadLog } from "./thread-blocks"

const toolName = "mcp_records_call_tool_0123456789ab"
const packedProgress = '{"agents":[{"role":"fixture","status":"running"}]}'

function liveCall(agentID = "manager", draftMessageId?: string) {
  const thread = { id: "thread", title: "Check records" }
  const wire = { v: 1, id: "", ok: true, thread_id: thread.id } as const
  let view = applyPush(openView(thread), { ...wire, op: OpReady, seq: 0, status: { running: true, turn_id: "turn" } })
  const push = (kind: string, seq: number, text: string, changes: Partial<RemoteEvent> = {}) => {
    const event = { kind, seq, text, thread_id: thread.id, turn_id: "turn", agent_id: agentID,
      tool_call_id: "call", created_at: "2026-11-07T00:00:00Z", ...changes }
    view = applyPush(view, { ...wire, op: OpEvent, seq, event })
  }
  if (draftMessageId) push("tool_call_delta", 0, `${toolName}(2)`, { message_id: draftMessageId })
  else push("tool_call", 1, `${toolName}({"name":"scan","arguments":{}})`)
  return { push, view: () => view, tool: () => view.blocks.find((block) => block.kind === "tool") }
}

it("shows the latest MCP status as plain text until the same call returns", () => {
  setLocale("en")
  const { push, view, tool } = liveCall()
  const { rerender } = render(<ThreadLog running blocks={view().blocks} />)
  const fold = screen.getByTestId("work-fold")
  push("tool_delta", 0, "Scanning the records")
  rerender(<ThreadLog running blocks={view().blocks} />)
  expect(fold).toHaveTextContent("Scanning the records")
  push("tool_delta", 0, "Another turn's progress", { turn_id: "old-turn" })
  push("tool_delta", 0, "Unbound progress", { tool_call_id: undefined })
  expect(tool()?.toolProgress).toBe("Scanning the records")

  push("tool_delta", 0, packedProgress)
  rerender(<ThreadLog running blocks={view().blocks} />)
  expect(fold).toHaveTextContent(packedProgress)
  expect(fold).not.toHaveTextContent("Scanning the records")
  expect(tool()?.text).toBe(toolName)
  fireEvent.click(fold)
  const chip = screen.getByRole("button", { name: new RegExp(`^${toolName}`) })
  expect(within(chip).getByText(packedProgress, { exact: true })).toBeVisible()
  fireEvent.click(chip)
  expect(screen.getByText(packedProgress, { exact: true, selector: "pre" })).toBeVisible()

  push("tool_result", 2, "Another turn's result", { turn_id: "old-turn" })
  expect(tool()).toMatchObject({ pending: true, toolProgress: packedProgress })
  push("tool_result", 3, "Records verified")
  push("tool_call_delta", 0, `${toolName}(44)`)
  push("tool_delta", 0, "Late progress must disappear")
  rerender(<ThreadLog running blocks={view().blocks} />)
  expect(screen.getByText("Records verified", { exact: true })).toBeVisible()
  expect(screen.queryByText(packedProgress, { exact: true })).not.toBeInTheDocument()
  expect(screen.queryByText("Late progress must disappear")).not.toBeInTheDocument()
  expect(view().blocks.filter((block) => block.kind === "tool")).toHaveLength(1)
  expect(tool()).toMatchObject({ pending: false, text: "Records verified" })
})

it("keeps the same worker's MCP call live across manager turns", () => {
  const { push, view, tool } = liveCall("worker")
  const nextTurn = { turn_id: "next-manager-turn" }
  push("done", 2, "", { agent_id: "manager" })
  push("turn", 3, "", { ...nextTurn, agent_id: "manager" })
  expect(view().detail?.running?.turn_id).toBe(nextTurn.turn_id)
  push("tool_delta", 0, "Worker is still scanning", nextTurn)
  push("tool_delta", 0, "Another worker's progress", { ...nextTurn, agent_id: "other-worker" })
  const workerBlocks = () => phoneAgents(view().blocks).find((agent) => agent.id === "worker")?.blocks ?? []
  const { rerender } = render(<ThreadLog running blocks={workerBlocks()} />)
  const fold = screen.getByTestId("work-fold")
  expect(fold).toHaveTextContent("Worker is still scanning")

  push("tool_result", 4, "Worker verified records", nextTurn)
  push("tool_delta", 0, "Late worker progress", nextTurn)
  rerender(<ThreadLog blocks={workerBlocks()} />)
  fireEvent.click(fold)
  expect(screen.getByText("Worker verified records", { exact: true })).toBeVisible()
  expect(screen.queryByText("Late worker progress")).not.toBeInTheDocument()
  expect(view().blocks.filter((block) => block.kind === "tool")).toHaveLength(1)
  expect(tool()).toMatchObject({ pending: false, text: "Worker verified records" })
  expect(tool()?.toolProgress).toBeUndefined()
})

it("reuses a worker's MCP argument draft when the call starts in the next manager turn", () => {
  const messageId = "worker-output"
  const { push, view, tool } = liveCall("worker", messageId)
  const nextTurn = { turn_id: "next-manager-turn", message_id: messageId }
  push("done", 1, "", { agent_id: "manager" })
  push("turn", 2, "", { ...nextTurn, agent_id: "manager" })
  push("tool_call_delta", 0, `${toolName}(24)`, nextTurn)
  push("tool_call", 3, `${toolName}({"name":"scan","arguments":{}})`, nextTurn)
  push("tool_delta", 0, "Scanning the records", nextTurn)
  push("tool_call_delta", 0, `${toolName}(24)`, nextTurn)
  expect(view().blocks.filter((block) => block.kind === "tool")).toHaveLength(1)
  expect(tool()).toMatchObject({ pending: true, writing: false, toolProgress: "Scanning the records" })

  push("tool_result", 4, "Worker verified records", nextTurn)
  push("tool_call_delta", 0, `${toolName}(24)`, nextTurn)
  push("tool_delta", 0, "Late worker progress", nextTurn)
  expect(view().blocks.filter((block) => block.kind === "tool")).toHaveLength(1)
  expect(tool()).toMatchObject({ pending: false, writing: false, text: "Worker verified records" })
  expect(tool()?.toolProgress).toBeUndefined()
})

it.each([
  ["manager Stop cleanup", "manager", "cleanup", {}],
  ["manager interruption", "manager", "error", { err: "interrupted" }],
  ["manager completion", "manager", "done", {}],
  ["worker finish", "worker", "finished", {}],
] as const)("clears pending MCP progress on %s", (_, agent, terminal, changes) => {
  const { push, view, tool } = liveCall(agent)
  push("tool_delta", 0, "Scanning the records")
  const { rerender } = render(<ThreadLog running blocks={view().blocks} />)
  expect(screen.getByTestId("work-fold")).toHaveTextContent("Scanning the records")

  push(terminal, 2, "", changes)
  push("tool_delta", 0, "Late progress must disappear")
  rerender(<ThreadLog blocks={view().blocks} />)
  expect(tool()).toMatchObject({ pending: false, text: toolName })
  expect(tool()?.toolProgress).toBeUndefined()
  expect(screen.queryByText("Scanning the records")).not.toBeInTheDocument()
  expect(screen.queryByText("Late progress must disappear")).not.toBeInTheDocument()
})
