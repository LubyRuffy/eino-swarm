import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Sidebar } from "./sidebar"

const noop = {
  onNew: vi.fn(),
  onOpen: vi.fn(),
  onRename: vi.fn(),
  onDelete: vi.fn(),
  onSearch: vi.fn(),
  onSettings: vi.fn(),
  onCollapse: vi.fn(),
  projects: [],
  onSelectProject: vi.fn(),
  onNewProject: vi.fn(),
  onEditProject: vi.fn(),
  onDeleteProject: vi.fn(),
  onOpenSkill: vi.fn(),
}

describe("Sidebar chrome", () => {
  // Hidden-inset macOS chrome sits on the first ~76px. New conversation lives
  // on the row below, the way Codex/Cursor do it, so the label is never under
  // the yellow blob.
  it("pads the chrome for traffic lights and keeps New conversation off that row", () => {
    render(<Sidebar threads={[]} trafficInset {...noop} />)
    const chrome = screen.getByTestId("sidebar-chrome")
    expect(chrome).toHaveClass("pl-traffic")
    expect(chrome).not.toHaveTextContent("New conversation")
    expect(screen.getByRole("button", { name: "New conversation" })).toBeInTheDocument()
  })

  it("does not pad in a browser, where there are no traffic lights", () => {
    render(<Sidebar threads={[]} {...noop} />)
    expect(screen.getByTestId("sidebar-chrome")).not.toHaveClass("pl-traffic")
    expect(screen.getByRole("button", { name: "New conversation" })).toBeInTheDocument()
  })

  it("hides the conversation list when the chrome toggle is clicked", () => {
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: "Hide conversations" }))
    expect(noop.onCollapse).toHaveBeenCalled()
  })
})
