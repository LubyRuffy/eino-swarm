import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Header } from "./header"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { ThreadStatus } from "@/lib/types"

const idle: ThreadStatus = { running: false }

function renderHeader(props: Partial<Parameters<typeof Header>[0]> = {}) {
  return render(
    <TooltipProvider>
      <Header
        status={idle}
        connected
        panelOpen
        sidebarOpen
        onTogglePanel={vi.fn()}
        onToggleSidebar={vi.fn()}
        onToggleTheme={vi.fn()}
        dark={false}
        {...props}
      />
    </TooltipProvider>,
  )
}

describe("Header sidebar toggle", () => {
  // When the list is gone the traffic lights sit on this bar, and the only
  // way back is a control that lives next to them — same as Cursor.
  it("offers to show the conversations when the sidebar is hidden", () => {
    const onToggleSidebar = vi.fn()
    renderHeader({ sidebarOpen: false, onToggleSidebar })
    fireEvent.click(screen.getByRole("button", { name: "Show conversations" }))
    expect(onToggleSidebar).toHaveBeenCalled()
  })

  it("pads for traffic lights when the sidebar is gone on desktop", () => {
    renderHeader({ sidebarOpen: false, trafficInset: true })
    expect(screen.getByRole("banner")).toHaveClass("pl-traffic")
  })

  it("does not show a second toggle while the sidebar is open", () => {
    renderHeader({ sidebarOpen: true })
    expect(screen.queryByRole("button", { name: "Show conversations" })).not.toBeInTheDocument()
  })
})

describe("Header project chip", () => {
  // Which directory the tools are pointed at is otherwise invisible, and it
  // is the difference between editing a scratch folder and editing a repo.
  it("names the project the conversation belongs to", () => {
    renderHeader({
      project: {
        id: "pj_1",
        name: "Anchored",
        system_prompt: "",
        workdir: "/home/me/repo",
        resolved_workdir: "/home/me/repo",
        memory_enabled: true,
        memory_dir: "/data/projects/pj_1/memory",
        created_at: "",
        updated_at: "",
      },
    })
    expect(screen.getByTestId("thread-project")).toHaveTextContent("Anchored")
  })

  it("shows nothing for a conversation with no project", () => {
    renderHeader()
    expect(screen.queryByTestId("thread-project")).not.toBeInTheDocument()
  })
})
