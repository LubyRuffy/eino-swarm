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

  it("always offers the Scheduled inbox section", () => {
    render(<Sidebar threads={[]} {...noop} />)
    const trigger = screen.getByRole("button", { name: /Scheduled/ })
    expect(trigger).toHaveAttribute("aria-haspopup", "dialog")
    expect(trigger.querySelector("[data-testid=section-fold]")).toBeNull()
    expect(screen.getByTestId("schedule-inbox")).toBeInTheDocument()
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
    expect(screen.getByRole("button", { name: "Projects" })).toHaveClass("px-2", "h-7")
    expect(screen.getByRole("button", { name: "Recents" })).toHaveClass("px-2", "h-7")
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
  // the pointer moved a pixel. Opening is a click; reorder is a title drag.
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

  it("does not paint a drag grip on Recents", () => {
    render(
      <Sidebar
        threads={[thread("th_a", "Alpha"), thread("th_b", "Beta")]}
        {...noop}
      />,
    )
    for (const row of screen.getAllByTestId("thread-row")) {
      expect(row.querySelector("[data-drag-handle]")).toBeNull()
    }
  })

  it("hides the sixth Recents conversation behind Show more", () => {
    render(
      <Sidebar
        threads={Array.from({ length: 6 }, (_, i) =>
          thread(`th_${i}`, `Loose ${i}`, {
            last_active_at: new Date(Date.now() - i * 60_000).toISOString(),
          }),
        )}
        {...noop}
      />,
    )
    expect(screen.getAllByTestId("thread-row")).toHaveLength(5)
    expect(screen.queryByText("Loose 5")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Show more" }))
    expect(screen.getByText("Loose 5")).toBeInTheDocument()
    expect(screen.getAllByTestId("thread-row")).toHaveLength(6)
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

  it("puts Recents running progress in the folder column", () => {
    render(
      <Sidebar
        threads={[thread("th_1", "Loose", { running: true })]}
        runningId="th_1"
        {...noop}
      />,
    )
    const kind = screen.getByTestId("row-kind")
    expect(kind.querySelector("[aria-label]")).toHaveAttribute("aria-label", "running")
    expect(screen.getByTestId("row-label").nextElementSibling).toBeNull()
  })

  it("marks a parked wait in the folder column so it does not look idle", () => {
    render(
      <Sidebar
        threads={[thread("th_1", "Loose")]}
        waitingIds={new Set(["th_1"])}
        {...noop}
      />,
    )
    expect(screen.getByTestId("wait-mark")).toHaveAttribute("aria-label", "waiting")
    expect(screen.queryByLabelText("running")).not.toBeInTheDocument()
  })

  it("keeps a live turn's progress when that conversation is also waiting", () => {
    render(
      <Sidebar
        threads={[thread("th_1", "Loose", { running: true })]}
        runningId="th_1"
        waitingIds={new Set(["th_1"])}
        {...noop}
      />,
    )
    expect(screen.getByLabelText("running")).toBeInTheDocument()
    expect(screen.queryByTestId("wait-mark")).not.toBeInTheDocument()
  })

  it("keeps a waiting conversation in the Recents preview", () => {
    render(
      <Sidebar
        threads={Array.from({ length: 6 }, (_, i) =>
          thread(`th_${i}`, `Loose ${i}`, {
            last_active_at: new Date(Date.now() - i * 60_000).toISOString(),
          }),
        )}
        waitingIds={new Set(["th_5"])}
        {...noop}
      />,
    )
    expect(screen.getByText("Loose 5")).toBeInTheDocument()
    expect(screen.getByTestId("wait-mark")).toBeInTheDocument()
  })

  it("keeps a running conversation visible when another project is open", () => {
    const openProject = {
      id: "pj_open",
      name: "Open",
      system_prompt: "",
      workdir: "",
      resolved_workdir: "/data/projects/pj_open/workspace",
      memory_enabled: true,
      memory_dir: "/data/projects/pj_open/memory",
      created_at: "",
      updated_at: "",
    }
    const busyProject = { ...openProject, id: "pj_busy", name: "Busy" }
    render(
      <Sidebar
        {...noop}
        threads={[
          thread("th_idle", "Idle topic", { project_id: "pj_open" }),
          thread("th_busy", "Busy topic", { project_id: "pj_busy", running: true }),
        ]}
        activeId="th_idle"
        projects={[openProject, busyProject]}
      />,
    )
    const busy = screen.getByText("Busy topic").closest("[data-testid=thread-row]")
    expect(busy).toBeInTheDocument()
    expect(busy?.querySelector("[aria-label]")).toHaveAttribute("aria-label", "running")
    expect(screen.getByText("Idle topic")).toBeInTheDocument()
  })

  it("does not offer pin on a Recents conversation", () => {
    render(<Sidebar threads={[thread("th_1", "Loose")]} {...noop} />)
    fireEvent.keyDown(screen.getByRole("button", { name: "More" }), {
      key: "ArrowDown",
    })
    expect(screen.queryByRole("menuitem", { name: "Pin" })).not.toBeInTheDocument()
  })

  // Recents has no folder glyph, but it still reserves the icon slot so
  // the title lines up with a project name.
  it("does not mark Recents with a project-topic icon", () => {
    render(<Sidebar threads={[thread("th_1", "Loose")]} activeId="th_1" {...noop} />)
    expect(screen.getByTestId("thread-row")).toHaveAttribute("aria-current", "true")
    expect(screen.getByTestId("thread-row")).toHaveClass("h-7")
    expect(screen.queryByTestId("thread-kind")).not.toBeInTheDocument()
    expect(screen.getByTestId("row-kind")).toHaveClass("size-4")
    expect(screen.getByTestId("row-kind")).toBeEmptyDOMElement()
    expect(
      screen.getByRole("button", { name: "Recents" }).querySelector("[data-testid=section-fold]"),
    ).toHaveClass("opacity-0")
    expect(
      screen.getByRole("button", { name: "Projects" }).querySelector("[data-testid=section-fold]"),
    ).toHaveClass("opacity-0")
  })

  it("folds Recents on the section header and remembers it", () => {
    const { unmount } = render(
      <Sidebar threads={[thread("th_1", "Hello")]} {...noop} />,
    )
    const recents = screen.getByRole("button", { name: "Recents" })
    expect(recents).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByText("Hello")).toBeInTheDocument()
    fireEvent.click(recents)
    expect(screen.queryByTestId("thread-row")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Recents" })).toHaveAttribute(
      "aria-expanded",
      "false",
    )
    expect(
      screen.getByRole("button", { name: "Recents" }).querySelector("[data-testid=section-fold]"),
    ).toHaveClass("opacity-100")
    unmount()
    render(<Sidebar threads={[thread("th_1", "Hello")]} {...noop} />)
    expect(screen.queryByTestId("thread-row")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Recents" })).toHaveAttribute(
      "aria-expanded",
      "false",
    )
  })

  it("folds Pinned and Projects the same way", () => {
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
          thread("th_loose", "Loose"),
        ]}
        {...noop}
        projects={[project]}
        selectedProjectId="pj_1"
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Pinned" }))
    expect(screen.getByTestId("pinned-list").querySelector('[data-testid="thread-row"]')).toBeNull()
    expect(screen.getByRole("button", { name: "First" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Projects" }))
    expect(screen.queryByRole("button", { name: "First" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "New project" })).toBeInTheDocument()
    expect(screen.getByText("Loose")).toBeInTheDocument()
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
