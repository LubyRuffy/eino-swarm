import { fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { Header } from "./header"
import { TooltipProvider } from "@/components/ui/tooltip"
import {
  SIDEBAR_WIDTH_DEFAULT,
  SIDEBAR_WIDTH_VAR,
} from "@/lib/sidebar-width"
import type { Thread, ThreadStatus } from "@/lib/types"
import { useApp } from "@/store/app"

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
        onOpenTerminal={vi.fn()}
        terminalOpen={false}
        terminalEnabled
        {...props}
      />
    </TooltipProvider>,
  )
}

describe("Header sidebar toggle", () => {
  // The toggle lives in the window title bar, next to the traffic lights,
  // whether the list is open or not — the Codex/Cursor chrome, not a
  // padded row inside the sidebar.
  it("offers to hide the conversations while the list is open", () => {
    const onToggleSidebar = vi.fn()
    renderHeader({ sidebarOpen: true, onToggleSidebar })
    fireEvent.click(screen.getByRole("button", { name: "Hide conversations" }))
    expect(onToggleSidebar).toHaveBeenCalled()
  })

  it("offers to show the conversations when the list is hidden", () => {
    const onToggleSidebar = vi.fn()
    renderHeader({ sidebarOpen: false, onToggleSidebar })
    fireEvent.click(screen.getByRole("button", { name: "Show conversations" }))
    expect(onToggleSidebar).toHaveBeenCalled()
  })

  it("keeps the leading cluster as wide as the sidebar so the title starts with the transcript", () => {
    renderHeader({ sidebarOpen: true })
    expect(screen.getByTestId("titlebar-leading").style.width).toBe(
      `var(${SIDEBAR_WIDTH_VAR}, ${SIDEBAR_WIDTH_DEFAULT}px)`,
    )
  })

  it("does not reserve the sidebar column once the list is gone", () => {
    renderHeader({ sidebarOpen: false })
    expect(screen.getByTestId("titlebar-leading").style.width).toBe("")
  })

  it("pads for traffic lights on the desktop title bar", () => {
    renderHeader({ trafficInset: true })
    expect(screen.getByTestId("titlebar-leading")).toHaveClass("pl-traffic")
  })

  it("does not pad in a browser, where there are no traffic lights", () => {
    renderHeader({ trafficInset: false })
    expect(screen.getByTestId("titlebar-leading")).not.toHaveClass("pl-traffic")
  })

  it("marks the bar as window chrome so a double-click can zoom", () => {
    renderHeader()
    expect(screen.getByRole("banner")).toHaveAttribute("data-drag-region")
  })
})

describe("Header project chip", () => {
  // Which directory the tools are pointed at is otherwise invisible, and it
  // is the difference between editing a scratch folder and editing a repo.
  // Two stacked lines in a 48px bar made the title look cramped; the prefix
  // has to stay on the same line as the conversation name.
  const project = {
    id: "pj_1",
    name: "Anchored",
    system_prompt: "",
    workdir: "/home/me/repo",
    resolved_workdir: "/home/me/repo",
    memory_enabled: true,
    memory_dir: "/data/projects/pj_1/memory",
    created_at: "",
    updated_at: "",
  }

  it("prefixes the title with the project name on one line", () => {
    renderHeader({ project })
    const name = screen.getByTestId("thread-project")
    const title = screen.getByTestId("thread-title")
    expect(name).toHaveTextContent("Anchored")
    expect(title).toHaveTextContent("New conversation")
    expect(name.compareDocumentPosition(title) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(title.parentElement).toContainElement(name)
    expect(title).toHaveClass("truncate")
    expect(title.className).toContain("font-normal")
    expect(title.className).toContain("--chrome-font-size")
    expect(title.className).not.toMatch(/\bfont-medium\b/)
    expect(title.parentElement).toHaveClass("items-center")
  })

  it("shows nothing for a conversation with no project", () => {
    renderHeader()
    expect(screen.queryByTestId("thread-project")).not.toBeInTheDocument()
    expect(screen.getByTestId("thread-title")).toHaveTextContent("New conversation")
    expect(screen.getByRole("button", { name: "Toggle side panel" })).toBeInTheDocument()
  })

  it("names the title bar Scheduled while that page is open", () => {
    useApp.setState({ scheduleInboxOpen: true })
    const thread: Thread = {
      id: "th_1",
      title: "A chat",
      project_id: "pj_1",
      provider_id: "default",
      reasoning_effort: "",
      archived: false,
      created_at: "",
      last_active_at: "",
      running: false,
    }
    renderHeader({ project, thread })
    expect(screen.getByTestId("thread-title")).toHaveTextContent("Scheduled")
    expect(screen.queryByTestId("thread-project")).not.toBeInTheDocument()
    expect(screen.queryByTestId("status-badge")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Toggle side panel" })).not.toBeInTheDocument()
  })
})

describe("Header status", () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it("says Waiting when the manager is paused at the tool-round cap", () => {
    renderHeader({
      status: {
        running: true,
        awaiting_continue: true,
        started_at: new Date(Date.now() - 5000).toISOString(),
      },
    })
    expect(screen.getByTestId("status-badge")).toHaveTextContent("Waiting")
  })

  it("says Your turn while ask_user is blocked", () => {
    renderHeader({
      status: {
        running: true,
        awaiting_answer: true,
        started_at: new Date(Date.now() - 5000).toISOString(),
      },
    })
    const badge = screen.getByTestId("status-badge")
    expect(badge).toHaveTextContent("Your turn")
    expect(badge).not.toHaveTextContent("Waiting")
    expect(badge).not.toHaveTextContent("Working")
    const mark = badge.querySelector("[data-testid=ask-mark]")
    expect(mark).toHaveAttribute("aria-label", "needs your answer")
    expect(mark?.querySelector(".animate-ping")).toBeTruthy()
  })

  it("says Waiting while a thread wake is parked", () => {
    renderHeader({ waiting: true })
    const badge = screen.getByTestId("status-badge")
    expect(badge).toHaveTextContent("Waiting")
    expect(badge.querySelector("[data-testid=wait-mark]")).toHaveAttribute(
      "aria-label",
      "waiting",
    )
    expect(badge).not.toHaveTextContent("Idle")
  })

  it("keeps Working when a turn is live even if a wait is also armed", () => {
    renderHeader({
      waiting: true,
      status: {
        running: true,
        started_at: new Date(Date.now() - 5000).toISOString(),
      },
    })
    expect(screen.getByTestId("status-badge")).toHaveTextContent("Working")
    expect(screen.queryByTestId("wait-mark")).not.toBeInTheDocument()
  })

  it("says Compressing while auto-compact is rewriting the next prompt", () => {
    renderHeader({
      status: {
        running: true,
        compressing: true,
        started_at: new Date(Date.now() - 5000).toISOString(),
      },
    })
    expect(screen.getByTestId("status-badge")).toHaveTextContent("Compressing")
    expect(screen.getByTestId("status-badge")).not.toHaveTextContent("Working")
  })

  it("keeps Your turn when the human has to answer, even during compact", () => {
    renderHeader({
      status: {
        running: true,
        compressing: true,
        awaiting_answer: true,
        started_at: new Date(Date.now() - 5000).toISOString(),
      },
    })
    expect(screen.getByTestId("status-badge")).toHaveTextContent("Your turn")
    expect(screen.getByTestId("status-badge")).not.toHaveTextContent("Waiting")
    expect(screen.getByTestId("status-badge")).not.toHaveTextContent("Compressing")
  })

  it("counts elapsed time from when the turn started, including hours", () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date("2026-01-01T10:10:06.000Z"))
    renderHeader({
      status: {
        running: true,
        started_at: "2026-01-01T00:00:00.000Z",
      },
    })
    expect(screen.getByTestId("status-badge")).toHaveTextContent("Working · 10h 10m")
  })

  it("does not invent a one-second clock when the start time is missing", () => {
    renderHeader({ status: { running: true } })
    expect(screen.getByTestId("status-badge")).toHaveTextContent("Working")
    expect(screen.getByTestId("status-badge")).not.toHaveTextContent("1s")
  })

})

describe("Header terminal", () => {
  it("opens a terminal in the current working directory on every click", () => {
    const onOpenTerminal = vi.fn()
    renderHeader({ onOpenTerminal })
    fireEvent.click(screen.getByRole("button", { name: "Open terminal" }))
    expect(onOpenTerminal).toHaveBeenCalled()
  })

  it("does not offer a shell when there is nowhere to start it", () => {
    renderHeader({ terminalEnabled: false })
    expect(screen.getByRole("button", { name: "Open terminal" })).toBeDisabled()
  })
})
