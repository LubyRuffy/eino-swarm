import { fireEvent, render, screen } from "@testing-library/react"
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
    expect(screen.getByTestId("client-task").querySelector("button")).toBeNull()
    expect(screen.queryByRole("button", { name: "fresh task" })).toBeNull()
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
})
