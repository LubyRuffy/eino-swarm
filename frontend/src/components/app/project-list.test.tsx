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
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
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
    onToggleSection: vi.fn(),
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
    const { onSelect, onToggle } = renderList()
    expect(screen.queryByRole("button", { name: "All conversations" })).not.toBeInTheDocument()
    const row = screen.getByRole("button", { name: "First" })
    expect(row).not.toHaveAttribute("aria-pressed")
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

  it("marks the open conversation, not the folder", () => {
    renderList({
      threadsByProject: {
        pj_1: [
          topic({ id: "th_open", title: "Open topic" }),
          topic({ id: "th_other", title: "Other topic" }),
        ],
      },
      expanded: { pj_1: true },
      activeId: "th_open",
    })
    expect(screen.getByRole("button", { name: "First" })).not.toHaveAttribute("aria-pressed")
    expect(screen.getByTestId("project-row").className.split(/\s+/)).not.toContain(
      "bg-sidebar-accent",
    )
    const openRow = screen.getByText("Open topic").closest("[data-testid=thread-row]")
    const otherRow = screen.getByText("Other topic").closest("[data-testid=thread-row]")
    expect(openRow).toHaveAttribute("aria-current", "true")
    expect(openRow?.querySelector("[data-testid=thread-kind]")).toBeNull()
    expect(otherRow).not.toHaveAttribute("aria-current")
    expect(otherRow?.querySelector("[data-testid=thread-kind]")).toBeNull()
  })

  // The directory glyph is the fold: open folder vs closed folder.
  // Topic titles keep the same size-4 slot so they sit under the name.
  it("swaps the folder glyph with expand, and lines topic names under the project name", () => {
    const threads = [
      topic({ id: "th_open", title: "Open topic" }),
      topic({ id: "th_other", title: "Other topic" }),
    ]
    const { rerender } = renderWith({
      threadsByProject: { pj_1: threads },
      expanded: { pj_1: true },
      activeId: "th_open",
    })
    expect(screen.queryByTestId("row-chevron")).not.toBeInTheDocument()
    expect(screen.queryByTestId("project-fold")).not.toBeInTheDocument()
    expect(screen.getByTestId("project-folder")).toHaveAttribute("data-open", "true")
    expect(screen.getByTestId("project-kind")).toHaveClass("size-4")
    const kinds = screen.getAllByTestId("row-kind")
    expect(kinds).toHaveLength(2)
    for (const slot of kinds) expect(slot).toHaveClass("size-4")
    expect(screen.getByTestId("project-row")).toHaveClass("pl-2")
    for (const row of screen.getAllByTestId("thread-row")) {
      expect(row).toHaveClass("pl-2")
      expect(row).not.toHaveClass("pl-5")
    }
    expect(
      screen.getByTestId("project-row").querySelector("[data-testid=row-label]"),
    ).toHaveTextContent("First")
    rerender({ threadsByProject: { pj_1: threads }, expanded: { pj_1: false } })
    expect(screen.getByTestId("project-folder")).toHaveAttribute("data-open", "false")
  })

  it("puts a running conversation's progress in the folder column", () => {
    renderList({
      threadsByProject: {
        pj_1: [topic({ id: "th_busy", title: "Busy topic", running: true })],
      },
      expanded: { pj_1: true },
      runningId: "th_busy",
    })
    const row = screen.getByTestId("thread-row")
    const kind = row.querySelector("[data-testid=row-kind]")
    expect(kind?.querySelector("[aria-label]")).toHaveAttribute("aria-label", "running")
    expect(row.querySelector("[data-testid=row-label]")?.nextElementSibling).toBeNull()
  })

  it("does not paint a drag grip on the folder or its topics", () => {
    renderList({
      threadsByProject: { pj_1: [topic({ id: "th_1", title: "A topic" })] },
      expanded: { pj_1: true },
    })
    expect(screen.getByTestId("project-row").querySelector("[data-drag-handle]")).toBeNull()
    expect(screen.getByTestId("thread-row").querySelector("[data-drag-handle]")).toBeNull()
  })

  // py-1.5 around a 28px menu button stacked each row to 40px.
  it("keeps folder and topic rows at the compact height", () => {
    renderList({
      threadsByProject: { pj_1: [topic({ id: "th_1", title: "A topic" })] },
      expanded: { pj_1: true },
    })
    expect(screen.getByTestId("project-row")).toHaveClass("h-7")
    expect(screen.getByTestId("thread-row")).toHaveClass("h-7")
    expect(screen.getByTestId("project-row")).not.toHaveClass("py-1.5")
    expect(screen.getByTestId("thread-row")).not.toHaveClass("py-1.5")
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

  it("folds the folder list from the Projects header without creating one", () => {
    const { onToggleSection, onNew } = renderList()
    const header = screen.getByRole("button", { name: "Projects" })
    expect(header).toHaveAttribute("aria-expanded", "true")
    fireEvent.click(header)
    expect(onToggleSection).toHaveBeenCalled()
    expect(onNew).not.toHaveBeenCalled()
  })

  it("hides every folder when the Projects section is collapsed", () => {
    renderList({ sectionOpen: false })
    expect(screen.queryByRole("button", { name: "First" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Projects" })).toHaveAttribute(
      "aria-expanded",
      "false",
    )
    expect(screen.getByRole("button", { name: "New project" })).toBeInTheDocument()
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
    fireEvent.mouseDown(rows[1])
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

  it("hides the sixth conversation behind Show more until it is opened", () => {
    const threads = Array.from({ length: 6 }, (_, i) =>
      topic({
        id: `th_${i}`,
        title: `Topic ${i}`,
        last_active_at: new Date(Date.now() - i * 60_000).toISOString(),
      }),
    )
    renderList({ threadsByProject: { pj_1: threads }, expanded: { pj_1: true } })
    expect(screen.getAllByTestId("thread-row")).toHaveLength(5)
    expect(screen.queryByText("Topic 5")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Show more" }))
    expect(screen.getAllByTestId("thread-row")).toHaveLength(6)
    expect(screen.getByText("Topic 5")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Show less" })).toHaveAttribute(
      "aria-expanded",
      "true",
    )
    fireEvent.click(screen.getByRole("button", { name: "Show less" }))
    expect(screen.queryByText("Topic 5")).not.toBeInTheDocument()
  })

  it("hides a conversation idle longer than a week even when the folder is short", () => {
    const stale = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000).toISOString()
    renderList({
      threadsByProject: {
        pj_1: [
          topic({ id: "th_fresh", title: "This week" }),
          topic({ id: "th_old", title: "Last month", last_active_at: stale }),
        ],
      },
      expanded: { pj_1: true },
    })
    expect(screen.getByText("This week")).toBeInTheDocument()
    expect(screen.queryByText("Last month")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Show more" }))
    expect(screen.getByText("Last month")).toBeInTheDocument()
  })

  it("keeps the open conversation visible when it would otherwise sit behind More", () => {
    const stale = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000).toISOString()
    renderList({
      threadsByProject: {
        pj_1: [
          topic({ id: "th_fresh", title: "This week" }),
          topic({
            id: "th_open",
            title: "Open last month",
            last_active_at: stale,
          }),
        ],
      },
      expanded: { pj_1: true },
      activeId: "th_open",
    })
    expect(screen.getByText("Open last month")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Show more" })).not.toBeInTheDocument()
  })

  it("reports the new topic order after a drop while older rows stay behind More", () => {
    const threads = Array.from({ length: 6 }, (_, i) =>
      topic({
        id: `th_${i}`,
        title: `Topic ${i}`,
        last_active_at: new Date(Date.now() - i * 60_000).toISOString(),
      }),
    )
    const { onReorderThreads } = renderList({
      threadsByProject: { pj_1: threads },
      expanded: { pj_1: true },
    })
    const rows = screen.getAllByTestId("thread-row")
    expect(rows).toHaveLength(5)
    const data: Record<string, string> = {}
    const dt = {
      setData: (type: string, value: string) => {
        data[type] = value
      },
      getData: (type: string) => data[type] ?? "",
      effectAllowed: "move",
      dropEffect: "move",
    }
    fireEvent.mouseDown(rows[4])
    fireEvent.dragStart(rows[4], { dataTransfer: dt })
    fireEvent.dragOver(rows[0], { dataTransfer: dt })
    fireEvent.drop(rows[0], { dataTransfer: dt })
    expect(onReorderThreads).toHaveBeenCalledWith([
      "th_4",
      "th_0",
      "th_1",
      "th_2",
      "th_3",
      "th_5",
    ])
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
    onToggleSection: vi.fn(),
  }
  const view = render(<ProjectList projects={projects} {...handlers} {...props} />)
  return {
    ...handlers,
    rerender: (next: Partial<Parameters<typeof ProjectList>[0]>) =>
      view.rerender(<ProjectList projects={projects} {...handlers} {...props} {...next} />),
  }
}
