import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Sidebar } from "./sidebar"
import {
  SIDEBAR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_VAR,
} from "@/lib/sidebar-width"
import type { Thread } from "@/lib/types"

const noop = {
  onNew: vi.fn(),
  onOpen: vi.fn(),
  onRename: vi.fn(),
  onDelete: vi.fn(),
  onSearch: vi.fn(),
  onSettings: vi.fn(),
  projects: [],
  onSelectProject: vi.fn(),
  onNewProject: vi.fn(),
  onNewInProject: vi.fn(),
  onEditProject: vi.fn(),
  onDeleteProject: vi.fn(),
  onOpenSkill: vi.fn(),
  onReorder: vi.fn(),
  onReorderProjects: vi.fn(),
  onPin: vi.fn(),
}

describe("Sidebar chrome", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    localStorage.clear()
    document.documentElement.style.removeProperty(SIDEBAR_WIDTH_VAR)
  })

  // The window title bar owns the traffic lights and the hide toggle.
  // New conversation is the first row of the list, so it never sits under
  // the yellow blob.
  it("starts with New conversation, not a title-bar chrome row", () => {
    render(<Sidebar threads={[]} {...noop} />)
    expect(screen.queryByTestId("sidebar-chrome")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Hide conversations" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: /^New conversation$/ })).toBeInTheDocument()
  })

  it("opens a new conversation from the first row", () => {
    render(<Sidebar threads={[]} {...noop} />)
    fireEvent.click(screen.getByRole("button", { name: /^New conversation$/ }))
    expect(noop.onNew).toHaveBeenCalled()
  })

  it("keeps Projects on the same gutter as Recents", () => {
    render(
      <Sidebar
        threads={[thread("th_1", "Hello")]}
        {...noop}
      />,
    )
    expect(screen.getByText("Projects")).toHaveClass("px-2")
    expect(screen.getByText("Recents")).toHaveClass("px-2")
    expect(screen.queryByText("Today")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "All conversations" })).not.toBeInTheDocument()
    expect(screen.getByTestId("project-list")).not.toHaveClass("px-2")
  })

  it("starts a conversation from a project row without using the list button", () => {
    const project = {
      id: "pj_1",
      name: "First",
      system_prompt: "",
      workdir: "",
      resolved_workdir: "/data/projects/pj_1/workspace",
      memory_enabled: true,
      memory_dir: "/data/projects/pj_1/memory",
      created_at: "",
      updated_at: "",
    }
    render(<Sidebar threads={[]} {...noop} projects={[project]} />)
    fireEvent.click(
      screen.getByRole("button", { name: "New conversation in First" }),
    )
    expect(noop.onNewInProject).toHaveBeenCalledWith(project)
    expect(noop.onNew).not.toHaveBeenCalled()
  })

  it("asks before deleting a conversation", () => {
    render(<Sidebar threads={[thread("th_1", "Hello")]} {...noop} />)
    // Radix opens on ArrowDown. A synthetic click opens then dismisses as
    // an outside click, and user-event will not click an opacity-0 trigger.
    fireEvent.keyDown(screen.getByRole("button", { name: "More" }), {
      key: "ArrowDown",
    })
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }))
    expect(noop.onDelete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(noop.onDelete).not.toHaveBeenCalled()
    fireEvent.keyDown(screen.getByRole("button", { name: "More" }), {
      key: "ArrowDown",
    })
    fireEvent.click(screen.getByRole("menuitem", { name: "Delete" }))
    fireEvent.click(screen.getByRole("button", { name: "Delete conversation" }))
    expect(noop.onDelete).toHaveBeenCalledWith("th_1")
  }, 15_000)
})

function thread(
  id: string,
  title: string,
  extra: Partial<Thread> = {},
): Thread {
  return {
    id,
    title,
    project_id: "",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    running: false,
    ...extra,
  }
}

function drag(from: HTMLElement, to: HTMLElement) {
  const data: Record<string, string> = {}
  const dt = {
    setData: (type: string, value: string) => {
      data[type] = value
    },
    getData: (type: string) => data[type] ?? "",
    effectAllowed: "move",
    dropEffect: "move",
  }
  const handle = from.querySelector("[data-drag-handle]")
  if (handle) fireEvent.mouseDown(handle)
  fireEvent.dragStart(from, { dataTransfer: dt })
  fireEvent.dragOver(to, { dataTransfer: dt })
  fireEvent.drop(to, { dataTransfer: dt })
  fireEvent.dragEnd(from, { dataTransfer: dt })
}

describe("Sidebar order", () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it("reports the new Recents order after a drop", () => {
    render(
      <Sidebar
        threads={[thread("th_a", "Alpha"), thread("th_b", "Beta")]}
        {...noop}
      />,
    )
    const rows = screen.getAllByTestId("thread-row")
    drag(rows[1], rows[0])
    expect(noop.onReorder).toHaveBeenCalledWith(["th_b", "th_a"])
  })

  it("does not reorder when the drag starts on the row menu", () => {
    render(
      <Sidebar
        threads={[thread("th_a", "Alpha"), thread("th_b", "Beta")]}
        {...noop}
      />,
    )
    const more = screen.getAllByRole("button", { name: "More" })[1]
    const first = screen.getAllByTestId("thread-row")[0]
    drag(more, first)
    expect(noop.onReorder).not.toHaveBeenCalled()
  })

  // A draggable row used to swallow the first click on the title whenever
  // the pointer moved a pixel. Opening is a click; reorder is the grip.
  it("opens on the first click of the title even if a dragstart races it", () => {
    render(
      <Sidebar
        threads={[thread("th_a", "Alpha"), thread("th_b", "Beta")]}
        {...noop}
      />,
    )
    const row = screen.getAllByTestId("thread-row")[0]
    const title = screen.getByRole("button", { name: "Alpha" })
    fireEvent.mouseDown(title)
    expect(row.draggable).toBe(false)
    fireEvent.dragStart(row)
    fireEvent.click(title)
    expect(noop.onOpen).toHaveBeenCalledWith("th_a")
    expect(noop.onReorder).not.toHaveBeenCalled()
  })

  it("opens from a click on the grip so the handle is not a dead zone", () => {
    render(
      <Sidebar
        threads={[thread("th_a", "Alpha"), thread("th_b", "Beta")]}
        {...noop}
      />,
    )
    fireEvent.click(
      screen.getAllByTestId("thread-row")[0].querySelector("[data-drag-handle]")!,
    )
    expect(noop.onOpen).toHaveBeenCalledWith("th_a")
  })
})

describe("Sidebar pin and folders", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  afterEach(() => {
    localStorage.clear()
  })

  it("tracks a pinned project topic at the top and still under its folder", () => {
    const project = {
      id: "pj_1",
      name: "First",
      system_prompt: "",
      workdir: "",
      resolved_workdir: "/data/projects/pj_1/workspace",
      memory_enabled: true,
      memory_dir: "/data/projects/pj_1/memory",
      created_at: "",
      updated_at: "",
    }
    render(
      <Sidebar
        threads={[
          thread("th_watch", "Watch this", {
            project_id: "pj_1",
            pinned: true,
            pinned_at: "2026-09-16T12:00:00Z",
          }),
        ]}
        {...noop}
        projects={[project]}
        selectedProjectId="pj_1"
      />,
    )
    expect(screen.getByTestId("pinned-list")).toHaveTextContent("Watch this")
    expect(screen.getByTestId("project-threads")).toHaveTextContent("Watch this")
    expect(screen.queryByTestId("recents-list")).not.toBeInTheDocument()
  })

  it("pins a project topic from the row menu", () => {
    const project = {
      id: "pj_1",
      name: "First",
      system_prompt: "",
      workdir: "",
      resolved_workdir: "/data/projects/pj_1/workspace",
      memory_enabled: true,
      memory_dir: "/data/projects/pj_1/memory",
      created_at: "",
      updated_at: "",
    }
    render(
      <Sidebar
        threads={[thread("th_1", "A topic", { project_id: "pj_1" })]}
        {...noop}
        projects={[project]}
        selectedProjectId="pj_1"
      />,
    )
    fireEvent.keyDown(screen.getByRole("button", { name: "More" }), {
      key: "ArrowDown",
    })
    fireEvent.click(screen.getByRole("menuitem", { name: "Pin" }))
    expect(noop.onPin).toHaveBeenCalledWith("th_1", true)
  })

  it("does not offer pin on a Recents conversation", () => {
    render(<Sidebar threads={[thread("th_1", "Loose")]} {...noop} />)
    fireEvent.keyDown(screen.getByRole("button", { name: "More" }), {
      key: "ArrowDown",
    })
    expect(screen.queryByRole("menuitem", { name: "Pin" })).not.toBeInTheDocument()
  })
})

describe("Sidebar resize", () => {
  afterEach(() => {
    localStorage.clear()
    document.documentElement.style.removeProperty(SIDEBAR_WIDTH_VAR)
  })

  it("starts at the default column and widens from the keyboard", () => {
    render(<Sidebar threads={[]} {...noop} />)
    const list = screen.getByTestId("conversation-list")
    const handle = screen.getByRole("separator", {
      name: "Resize the conversation list",
    })
    expect(list.style.width).toBe(
      `var(${SIDEBAR_WIDTH_VAR}, ${SIDEBAR_WIDTH_DEFAULT}px)`,
    )
    fireEvent.keyDown(handle, { key: "ArrowRight" })
    expect(document.documentElement.style.getPropertyValue(SIDEBAR_WIDTH_VAR)).toBe(
      `${SIDEBAR_WIDTH_DEFAULT + 24}px`,
    )
    expect(localStorage.getItem("zwai.sidebar.width")).toBe(
      String(SIDEBAR_WIDTH_DEFAULT + 24),
    )
  })

  it("restores a remembered width so a reload is not a reset", () => {
    localStorage.setItem("zwai.sidebar.width", "320")
    render(<Sidebar threads={[]} {...noop} />)
    expect(document.documentElement.style.getPropertyValue(SIDEBAR_WIDTH_VAR)).toBe(
      "320px",
    )
    expect(screen.getByTestId("conversation-list").style.width).toBe(
      `var(${SIDEBAR_WIDTH_VAR}, 320px)`,
    )
  })

  // The strip hangs 4px into the transcript. The list is the earlier flex
  // sibling, so without a stacking context the main column paints over it.
  it("keeps the drag strip above the transcript", () => {
    render(<Sidebar threads={[]} {...noop} />)
    const handle = screen.getByRole("separator", {
      name: "Resize the conversation list",
    })
    expect(handle.className).toMatch(/\bz-20\b/)
    expect(handle.className).toMatch(/-right-1/)
    expect(screen.getByTestId("conversation-list").className).toMatch(/\bz-10\b/)
  })
})
