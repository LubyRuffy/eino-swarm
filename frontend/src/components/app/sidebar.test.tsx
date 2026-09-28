import { act, fireEvent, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { Sidebar } from "./sidebar"
import { setDeskDest } from "@/lib/desk-nav"
import {
  SIDEBAR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_VAR,
} from "@/lib/sidebar-width"
import type { Thread } from "@/lib/types"

vi.mock("@/lib/api", () => ({
  api: {
    clients: async () => ({ enabled: false, pending: false, tools: [] }),
  },
}))

const noop = {
  onNew: vi.fn(),
  onOpen: vi.fn(),
  onRename: vi.fn(),
  onDelete: vi.fn(),
  onSearch: vi.fn(),
  onSettings: vi.fn(),
  onToggleTheme: vi.fn(),
  onToggleLocale: vi.fn(),
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
  // Projects is the home list. A loose new conversation lives on
  // Conversations, so it never sits under the yellow blob on this page.
  it("does not offer a loose new conversation on the project list", () => {
    render(<Sidebar threads={[]} {...noop} />)
    expect(screen.queryByTestId("sidebar-chrome")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Hide conversations" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /^New conversation$/ })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Search conversations (⌘K)" })).toBeInTheDocument()
  })

  it("keeps Scheduled on the leftmost rail, not as a fold in the chat list", () => {
    render(<Sidebar threads={[]} {...noop} />)
    const trigger = screen.getByRole("button", { name: /Scheduled/ })
    expect(screen.getByTestId("dest-rail")).toContainElement(trigger)
    expect(trigger).not.toHaveAttribute("aria-haspopup")
    expect(trigger.querySelector("[data-testid=section-fold]")).toBeNull()
    expect(trigger).not.toHaveClass("sidebar-section-label")
    expect(screen.getByTestId("schedule-inbox")).toBeInTheDocument()
    expect(screen.queryByTestId("clients-list")).not.toBeInTheDocument()
  })

  it("opens a new conversation from the Conversations list", () => {
    render(<Sidebar threads={[]} {...noop} />)
    act(() => setDeskDest("chats"))
    const button = screen.getByRole("button", { name: /^New conversation$/ })
    expect(button).toHaveStyle({ height: "var(--sidebar-row-height)" })
    fireEvent.click(button)
    expect(noop.onNew).toHaveBeenCalled()
  })

  it("paints Settings as a full-width hover pill, not a tiny ghost chip", () => {
    render(<Sidebar threads={[]} {...noop} />)
    const button = screen.getByRole("button", { name: "Settings" })
    expect(button.className).toMatch(/\bflex-1\b/)
    expect(screen.getByRole("button", { name: "App menu" })).toBeInTheDocument()
    expect(button.className).toMatch(/\brounded-full\b/)
    expect(button.className).toMatch(/hover:bg-sidebar-accent/)
    expect(button).toHaveStyle({ height: "var(--sidebar-row-height)" })
    fireEvent.click(button)
    expect(noop.onSettings).toHaveBeenCalled()
  })

  it("keeps Conversations off the project list", () => {
    const view = render(
      <Sidebar
        threads={[thread("th_1", "Hello")]}
        {...noop}
      />,
    )
    const projects = within(screen.getByTestId("project-list")).getByRole("button", {
      name: "Projects",
    })
    expect(projects).toHaveClass("sidebar-section-label")
    expect(screen.queryByTestId("recents-list")).not.toBeInTheDocument()
    expect(screen.queryByText("Hello")).not.toBeInTheDocument()
    expect(screen.queryByText("Today")).not.toBeInTheDocument()
    expect(screen.getByTestId("project-list")).not.toHaveClass("px-2")
    act(() => setDeskDest("chats"))
    view.rerender(<Sidebar threads={[thread("th_1", "Hello")]} {...noop} />)
    const conversations = within(screen.getByTestId("recents-list")).getByRole("button", {
      name: "Conversations",
    })
    expect(conversations).toHaveClass("sidebar-section-label")
    expect(screen.queryByTestId("project-list")).not.toBeInTheDocument()
    expect(screen.getByText("Hello")).toBeInTheDocument()
  })

  it("starts a loose conversation from the Conversations header", () => {
    setDeskDest("chats")
    render(<Sidebar threads={[thread("th_1", "Hello")]} {...noop} />)
    fireEvent.click(screen.getByTestId("recents-new"))
    expect(noop.onNew).toHaveBeenCalled()
    expect(
      within(screen.getByTestId("recents-list")).getByRole("button", { name: "Conversations" }),
    ).toHaveAttribute("aria-expanded", "true")
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
    setDeskDest("chats")
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
    setDeskDest("chats")
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
    setDeskDest("chats")
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
    setDeskDest("chats")
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
    setDeskDest("chats")
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

  it("marks a blocked ask as your turn instead of the working pulse", () => {
    setDeskDest("chats")
    render(
      <Sidebar
        threads={[thread("th_1", "Loose", { running: true })]}
        runningId="th_1"
        askingIds={new Set(["th_1"])}
        {...noop}
      />,
    )
    const mark = screen.getByTestId("ask-mark")
    expect(mark).toHaveAttribute("aria-label", "needs your answer")
    expect(mark.querySelector(".animate-ping")).toBeTruthy()
    expect(screen.queryByLabelText("running")).not.toBeInTheDocument()
    expect(screen.queryByTestId("wait-mark")).not.toBeInTheDocument()
  })

  it("keeps an asking conversation in the Recents preview", () => {
    setDeskDest("chats")
    render(
      <Sidebar
        threads={Array.from({ length: 6 }, (_, i) =>
          thread(`th_${i}`, `Loose ${i}`, {
            last_active_at: new Date(Date.now() - i * 60_000).toISOString(),
          }),
        )}
        askingIds={new Set(["th_5"])}
        {...noop}
      />,
    )
    expect(screen.getByText("Loose 5")).toBeInTheDocument()
    expect(screen.getByTestId("ask-mark")).toBeInTheDocument()
  })

  it("keeps a waiting conversation in the Recents preview", () => {
    setDeskDest("chats")
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
    setDeskDest("chats")
    render(<Sidebar threads={[thread("th_1", "Loose")]} {...noop} />)
    fireEvent.keyDown(screen.getByRole("button", { name: "More" }), {
      key: "ArrowDown",
    })
    expect(screen.queryByRole("menuitem", { name: "Pin" })).not.toBeInTheDocument()
  })

  // Recents has no folder glyph, but it still reserves the icon slot so
  // the title lines up with a project name.
  it("does not mark Recents with a project-topic icon", () => {
    setDeskDest("chats")
    render(<Sidebar threads={[thread("th_1", "Loose")]} activeId="th_1" {...noop} />)
    expect(screen.getByTestId("thread-row")).toHaveAttribute("aria-current", "true")
    expect(screen.getByTestId("thread-row")).toHaveClass("sidebar-row")
    expect(screen.getByTestId("thread-row")).not.toHaveClass("h-7")
    expect(screen.queryByTestId("thread-kind")).not.toBeInTheDocument()
    expect(screen.getByTestId("row-kind")).toHaveClass("sidebar-kind")
    expect(screen.getByTestId("row-kind")).toBeEmptyDOMElement()
    expect(
      within(screen.getByTestId("recents-list"))
        .getByRole("button", { name: "Conversations" })
        .querySelector("[data-testid=section-fold]"),
    ).toHaveClass("opacity-0")
  })

  it("folds Recents on the section header and remembers it", () => {
    setDeskDest("chats")
    const { unmount } = render(
      <Sidebar threads={[thread("th_1", "Hello")]} {...noop} />,
    )
    const recents = within(screen.getByTestId("recents-list")).getByRole("button", {
      name: "Conversations",
    })
    expect(recents).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByText("Hello")).toBeInTheDocument()
    fireEvent.click(recents)
    expect(screen.queryByTestId("thread-row")).not.toBeInTheDocument()
    expect(recents).toHaveAttribute("aria-expanded", "false")
    expect(recents.querySelector("[data-testid=section-fold]")).toHaveClass("opacity-100")
    unmount()
    render(<Sidebar threads={[thread("th_1", "Hello")]} {...noop} />)
    const again = within(screen.getByTestId("recents-list")).getByRole("button", {
      name: "Conversations",
    })
    expect(screen.queryByTestId("thread-row")).not.toBeInTheDocument()
    expect(again).toHaveAttribute("aria-expanded", "false")
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
    fireEvent.click(
      within(screen.getByTestId("project-list")).getByRole("button", { name: "Projects" }),
    )
    expect(screen.queryByRole("button", { name: "First" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "New project" })).toBeInTheDocument()
    expect(screen.queryByText("Loose")).not.toBeInTheDocument()
  })
})

describe("Sidebar list column", () => {
  it("hides the list and keeps the icon rail", () => {
    render(<Sidebar threads={[]} {...noop} listOpen={false} />)
    expect(screen.getByTestId("dest-rail")).toBeInTheDocument()
    expect(screen.getByTestId("dest-projects")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: /^New conversation$/ })).not.toBeInTheDocument()
    expect(screen.queryByTestId("project-list")).not.toBeInTheDocument()
    expect(screen.queryByRole("separator", { name: "Resize the conversation list" })).not.toBeInTheDocument()
    expect(screen.getByTestId("conversation-list").style.width).toBe(
      "var(--dest-rail-width)",
    )
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
