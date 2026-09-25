import { render, screen } from "@testing-library/react"
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
})
