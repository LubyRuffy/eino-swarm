import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ClientGroups } from "./client-groups"

describe("ClientGroups", () => {
  it("shows a running light and does not open the task", () => {
    const onMore = vi.fn()
    render(
      <ClientGroups
        tools={[
          {
            id: "codex",
            more: false,
            tasks: [{ id: "c1", title: "fresh task", status: "running", updated_at: "2026-09-25T00:00:00Z" }],
          },
        ]}
        onMore={onMore}
      />,
    )
    expect(screen.getByTestId("client-status")).toHaveAttribute("data-status", "running")
    fireEvent.click(screen.getByRole("button", { name: "fresh task" }))
    expect(onMore).not.toHaveBeenCalled()
    expect(screen.getByTestId("client-transcript")).toBeTruthy()
    expect(screen.getByLabelText("Message")).toBeDisabled()
  })

  it("folds a tool group and hides its tasks", () => {
    render(
      <ClientGroups
        tools={[
          {
            id: "claude",
            more: false,
            tasks: [{ id: "c1", title: "open session", status: "done", updated_at: "2026-09-25T00:00:00Z" }],
          },
        ]}
        onMore={() => undefined}
      />,
    )
    expect(screen.getByTestId("client-task")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Claude" }))
    expect(screen.queryByTestId("client-task")).toBeNull()
  })

  it("folds adjacent thinking and tools until the row is opened", async () => {
    render(
      <ClientGroups
        tools={[
          {
            id: "cursor",
            more: false,
            tasks: [{ id: "c1", title: "open session", status: "done", updated_at: "2026-09-25T00:00:00Z" }],
          },
        ]}
        onMore={() => undefined}
        onRead={async () => ({
          id: "c1",
          title: "open session",
          status: "done",
          entries: [
            { role: "user", text: "the request" },
            { role: "thinking", text: "checked the path" },
            { role: "tool", text: "read" },
            { role: "assistant", text: "done" },
          ],
        })}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "open session" }))
    const fold = await screen.findByTestId("work-fold")
    expect(screen.getByTestId("client-request")).toHaveTextContent("the request")
    expect(fold).toHaveTextContent("Thought · 1 tool")
    expect(screen.queryByText("read")).toBeNull()
    expect(screen.getByText("done")).toBeTruthy()
    fireEvent.click(fold)
    await waitFor(() => expect(screen.getByText("read")).toBeTruthy())
    expect(screen.getByText("checked the path")).toBeTruthy()
  })
})
