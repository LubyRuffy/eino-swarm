import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ProjectList } from "./project-list"
import type { Project, Thread } from "@/lib/types"

const projects: Project[] = [
  {
    id: "pj_1",
    name: "First",
    system_prompt: "",
    workdir: "",
    resolved_workdir: "/data/projects/pj_1/workspace",
    memory_enabled: true,
    memory_dir: "/data/projects/pj_1/memory",
    created_at: "",
    updated_at: "",
  },
]

function topic(partial: Partial<Thread> & { id: string; title: string }): Thread {
  return {
    project_id: "pj_1",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: "2026-09-16T00:00:00Z",
    last_active_at: "2026-09-16T12:00:00Z",
    running: false,
    sort_rank: 0,
    ...partial,
  }
}

function renderList(props: Partial<Parameters<typeof ProjectList>[0]> = {}) {
  const handlers = {
    threadsByProject: {} as Record<string, Thread[]>,
    expanded: { pj_1: true },
    onSelect: vi.fn(),
    onToggle: vi.fn(),
    onNew: vi.fn(),
    onNewConversation: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onOpenSkill: vi.fn(),
    onReorder: vi.fn(),
    onOpenThread: vi.fn(),
    onRenameThread: vi.fn(),
    onDeleteThread: vi.fn(),
    onReorderThreads: vi.fn(),
    onPinThread: vi.fn(),
  }
  render(<ProjectList projects={projects} {...handlers} {...props} />)
  return handlers
}

describe("Project list", () => {
  // The sidebar scrollport already has px-2. Wrapping this section in
  // another one pushed Projects 8px further in than Recents.
  it("shares the conversation list gutter instead of adding its own", () => {
    renderList()
    expect(screen.getByTestId("project-list")).not.toHaveClass("px-2")
  })

  it("toggles a project folder instead of filtering the whole list", () => {
    const { onSelect, onToggle } = renderList({ selectedId: "pj_1" })
    expect(screen.queryByRole("button", { name: "All conversations" })).not.toBeInTheDocument()
    const row = screen.getByRole("button", { name: "First" })
    expect(row).toHaveAttribute("aria-pressed", "true")
    expect(row).toHaveAttribute("aria-expanded", "true")
    fireEvent.click(row)
    expect(onToggle).toHaveBeenCalledWith("pj_1")
    expect(onSelect).toHaveBeenCalledWith("pj_1")
  })

  it("nests conversations under an expanded project and hides them when collapsed", () => {
    const threads = [topic({ id: "th_1", title: "A topic" })]
    const { rerender } = renderWith(
      { threadsByProject: { pj_1: threads }, expanded: { pj_1: true } },
    )
    expect(screen.getByTestId("project-threads")).toBeInTheDocument()
    expect(screen.getByText("A topic")).toBeInTheDocument()
    rerender({ threadsByProject: { pj_1: threads }, expanded: { pj_1: false } })
    expect(screen.queryByTestId("project-threads")).not.toBeInTheDocument()
    expect(screen.queryByText("A topic")).not.toBeInTheDocument()
  })

  it("offers a new project and a menu on each row", () => {
    const { onNew } = renderList()
    fireEvent.click(screen.getByRole("button", { name: "New project" }))
    expect(onNew).toHaveBeenCalled()
    expect(
      screen.getByRole("button", { name: "Project options for First" }),
    ).toBeInTheDocument()
  })

  // The row is the directory. Reaching past it to the list's New conversation
  // would land work in Recents instead of this folder.
  it("starts a conversation in the project the row names", () => {
    const { onNewConversation, onSelect } = renderList()
    fireEvent.click(
      screen.getByRole("button", { name: "New conversation in First" }),
    )
    expect(onNewConversation).toHaveBeenCalledWith(
      expect.objectContaining({ id: "pj_1", name: "First" }),
    )
    expect(onSelect).not.toHaveBeenCalled()
    expect(
      screen.getByRole("button", { name: "New conversation in First" }),
    ).toHaveClass("opacity-0", "group-hover:opacity-100")
  })

  it("explains what a project is when there are none", () => {
    renderList({ projects: [] })
    expect(screen.getByText(/one working directory/)).toBeInTheDocument()
  })

  it("opens skills from the row menu instead of listing them under the name", () => {
    const { onOpenSkill } = renderList({
      projects: [
        {
          ...projects[0],
          skills: [{ name: "a-procedure", description: "when it applies", updated_at: "" }],
        },
      ],
    })
    expect(screen.queryByTestId("project-skills")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Open skill a-procedure" })).not.toBeInTheDocument()
    fireEvent.keyDown(
      screen.getByRole("button", { name: "Project options for First" }),
      { key: "ArrowDown" },
    )
    fireEvent.click(screen.getByRole("menuitem", { name: "View skills" }))
    expect(onOpenSkill).toHaveBeenCalledWith(
      expect.objectContaining({ id: "pj_1" }),
    )
  })

  it("reports the new project order after a drop", () => {
    const second: Project = { ...projects[0], id: "pj_2", name: "Second" }
    const { onReorder } = renderList({
      projects: [...projects, second],
      expanded: { pj_1: false, pj_2: false },
    })
    const rows = screen.getAllByTestId("project-row")
    const data: Record<string, string> = {}
    const dt = {
      setData: (type: string, value: string) => {
        data[type] = value
      },
      getData: (type: string) => data[type] ?? "",
      effectAllowed: "move",
      dropEffect: "move",
    }
    fireEvent.mouseDown(rows[1].querySelector("[data-drag-handle]")!)
    fireEvent.dragStart(rows[1], { dataTransfer: dt })
    fireEvent.dragOver(rows[0], { dataTransfer: dt })
    fireEvent.drop(rows[0], { dataTransfer: dt })
    expect(onReorder).toHaveBeenCalledWith(["pj_2", "pj_1"])
  })

  it("selects on the first click of the name even if a dragstart races it", () => {
    const { onSelect, onReorder, onToggle } = renderList()
    const row = screen.getByTestId("project-row")
    const name = screen.getByRole("button", { name: "First" })
    fireEvent.mouseDown(name)
    expect(row.draggable).toBe(false)
    fireEvent.dragStart(row)
    fireEvent.click(name)
    expect(onSelect).toHaveBeenCalledWith("pj_1")
    expect(onToggle).toHaveBeenCalledWith("pj_1")
    expect(onReorder).not.toHaveBeenCalled()
  })
})

function renderWith(props: Partial<Parameters<typeof ProjectList>[0]>) {
  const handlers = {
    threadsByProject: {} as Record<string, Thread[]>,
    expanded: { pj_1: true },
    onSelect: vi.fn(),
    onToggle: vi.fn(),
    onNew: vi.fn(),
    onNewConversation: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onOpenSkill: vi.fn(),
    onReorder: vi.fn(),
    onOpenThread: vi.fn(),
    onRenameThread: vi.fn(),
    onDeleteThread: vi.fn(),
    onReorderThreads: vi.fn(),
    onPinThread: vi.fn(),
  }
  const view = render(<ProjectList projects={projects} {...handlers} {...props} />)
  return {
    ...handlers,
    rerender: (next: Partial<Parameters<typeof ProjectList>[0]>) =>
      view.rerender(<ProjectList projects={projects} {...handlers} {...props} {...next} />),
  }
}
