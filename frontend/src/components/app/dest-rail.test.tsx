import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { DestRail, useVisibleDest } from "./dest-rail"
import { useApp } from "@/store/app"
import { LocalClientsSection } from "./local-clients"
import { ScheduleListPane } from "./schedule-inbox"
import type { ClientTool } from "@/lib/local-clients"

const fake = vi.hoisted(() => ({
  enabled: false,
  tools: [] as ClientTool[],
}))

vi.mock("@/lib/api", () => ({
  api: {
    clients: async () => ({ enabled: fake.enabled, pending: false, tools: fake.tools }),
    schedules: async () => ({ schedules: [], unread: 0 }),
  },
}))

function Probe() {
  const pane = useVisibleDest()
  return (
    <div>
      <DestRail />
      <div data-testid="pane">{pane}</div>
      {pane === "scheduled" ? <ScheduleListPane /> : null}
      {pane === "clients" ? <LocalClientsSection bare /> : null}
    </div>
  )
}

describe("destination rail", () => {
  beforeEach(() => {
    fake.enabled = false
    fake.tools = []
  })

  it("switches the list without stacking waits and clients under chats", async () => {
    fake.enabled = true
    fake.tools = [
      { id: "codex", more: false, tasks: [{ id: "c1", title: "foreign", status: "done", updated_at: "2026-09-25T00:00:00Z" }] },
    ]
    render(<Probe />)
    expect(screen.getByTestId("pane")).toHaveTextContent("projects")
    expect(screen.getByTestId("dest-projects")).toHaveAttribute("aria-current", "page")
    expect(screen.queryByTestId("clients-list")).not.toBeInTheDocument()

    fireEvent.click(screen.getByTestId("schedule-inbox"))
    expect(screen.getByTestId("pane")).toHaveTextContent("scheduled")
    expect(screen.getByTestId("schedule-list-pane")).toBeInTheDocument()
    expect(screen.queryByTestId("clients-list")).not.toBeInTheDocument()

    fireEvent.click(await screen.findByTestId("dest-clients"))
    expect(screen.getByTestId("pane")).toHaveTextContent("clients")
    expect(screen.getByTestId("clients-list")).toBeInTheDocument()
    expect(screen.getByTestId("client-tool-codex")).toBeInTheDocument()
    expect(screen.queryByTestId("schedule-list-pane")).not.toBeInTheDocument()

    fireEvent.click(screen.getByTestId("dest-chats"))
    await waitFor(() => expect(screen.getByTestId("pane")).toHaveTextContent("chats"))
    expect(screen.queryByTestId("clients-list")).not.toBeInTheDocument()

    fireEvent.click(screen.getByTestId("dest-projects"))
    await waitFor(() => expect(screen.getByTestId("pane")).toHaveTextContent("projects"))
  })

  it("does not paint an unread count on the rail", () => {
    useApp.setState({ scheduleUnread: 726 })
    render(<DestRail />)
    expect(screen.queryByTestId("schedule-unread")).not.toBeInTheDocument()
    expect(screen.getByTestId("schedule-inbox")).toHaveAttribute("aria-label", "Scheduled")
    const rail = screen.getByTestId("dest-rail")
    expect(rail.style.paddingLeft).toBe(
      "calc(var(--sidebar-list-px) + var(--sidebar-row-px))",
    )
    expect(rail.style.paddingRight).toBe("var(--sidebar-list-px)")
  })
})
