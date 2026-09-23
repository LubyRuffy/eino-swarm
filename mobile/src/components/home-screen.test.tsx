import { act, fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { HomeScreen } from "./home-screen"
import type { SavedLink } from "@/lib/store"

function chrome(): {
  hosts: SavedLink[]
  activeFingerprint: string
  onSelectHost: () => void
  onAddHost: () => void
} {
  return {
    hosts: [
      {
        hubURL: "https://hub.example.test",
        ticket: "t",
        hostPub: "p",
        sessionID: "s",
        fingerprint: "fp",
      },
    ],
    activeFingerprint: "fp",
    onSelectHost: vi.fn(),
    onAddHost: vi.fn(),
  }
}

describe("HomeScreen", () => {
  it("lists five threads, in-progress, and find-and-start instead of a message box", () => {
    const onOpen = vi.fn()
    const onNewChat = vi.fn()
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[{ id: "p", name: "work" }]}
        threads={Array.from({ length: 5 }, (_, i) => ({
          id: "t" + i,
          title: "thread " + i,
          running: false,
          last_active_at: "2026-01-01T00:00:00Z",
          summary: "one line",
        }))}
        running={[{ thread_id: "t0", title: "thread 0", action: "read" }]}
        more
        onOpen={onOpen}
        onMore={vi.fn()}
        onNewChat={onNewChat}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByLabelText("path=relay")).toBeInTheDocument()
    expect(screen.getByText("In progress")).toBeInTheDocument()
    expect(screen.getByText("thread 4")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "More" })).toBeInTheDocument()
    // Reading what is already running is the inbox's job. A message box here
    // asked which PC and which project before either had been chosen.
    expect(screen.queryByLabelText("New message")).not.toBeInTheDocument()
    expect(screen.queryByRole("radiogroup", { name: "Project" })).not.toBeInTheDocument()
    expect(screen.getByLabelText("Search conversations")).toBeInTheDocument()
    fireEvent.click(screen.getByTestId("new-chat"))
    expect(onNewChat).toHaveBeenCalledWith("")
    fireEvent.click(screen.getByRole("button", { name: "Open thread 2" }))
    expect(onOpen).toHaveBeenCalledWith("t2")
    expect(screen.getAllByText("thread 0")).toHaveLength(1)
  })

  it("marks a dead socket so a quiet inbox is not mistaken for live", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        connected={false}
        projects={[]}
        threads={[
          {
            id: "t1",
            title: "thread 1",
            running: false,
            last_active_at: "2026-01-01T00:00:00Z",
          },
        ]}
        running={[]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByLabelText("path=offline")).toBeInTheDocument()
    expect(screen.queryByLabelText("path=relay")).not.toBeInTheDocument()
  })

  it("treats a listing running flag as in-progress", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[
          {
            id: "t1",
            title: "thread 1",
            running: true,
            last_active_at: "2026-01-01T00:00:00Z",
            summary: "one line",
          },
        ]}
        running={[]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByText("In progress")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Open thread 1" })).toBeInTheDocument()
    expect(screen.queryByText("Recent")).not.toBeInTheDocument()
  })

  it("lists a parked wait as in-progress instead of a quiet recent row", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[
          {
            id: "t1",
            title: "thread 1",
            running: false,
            waiting: true,
            last_active_at: "2026-01-01T00:00:00Z",
            summary: "one line",
          },
        ]}
        running={[]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByText("In progress")).toBeInTheDocument()
    expect(screen.getByText("Waiting")).toBeInTheDocument()
    expect(screen.queryByText("Recent")).not.toBeInTheDocument()
  })

  it("does not dump schedule tool JSON on a live row", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[]}
        running={[
          {
            thread_id: "t1",
            title: "thread 1",
            turn_id: "tu",
            waiting: true,
            action: `schedule_wake({"every_s":900,"id":"sch_x","prompt":"Continue the wait."})`,
          },
        ]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.queryByText(/schedule_wake/)).not.toBeInTheDocument()
    expect(screen.queryByText(/sch_x/)).not.toBeInTheDocument()
    expect(screen.getByText("Waiting")).toBeInTheDocument()
  })

  it("paints findings instead of a report_schedule envelope on inbox rows", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[
          {
            id: "t2",
            title: "thread 2",
            running: false,
            last_active_at: "2026-01-01T00:00:00Z",
            summary: `report_schedule({"findings":"one thing changed"})`,
          },
        ]}
        running={[
          {
            thread_id: "t1",
            title: "thread 1",
            action: `report_schedule({"findings":"one thing changed","keep":true})`,
          },
        ]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getAllByText("one thing changed")).toHaveLength(2)
    expect(screen.queryByText(/report_schedule/)).not.toBeInTheDocument()
  })

  // A project with no threads is still a place a conversation can start.
  // Recent and In progress are not projects, so they do not grow that control.
  it("starts in the project whose row was tapped, including one with no threads", () => {
    const onNewChat = vi.fn()
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[
          { id: "p1", name: "work" },
          { id: "p2", name: "notes" },
        ]}
        threads={[
          {
            id: "t1",
            title: "alpha",
            project_id: "p1",
            running: false,
            last_active_at: "2026-01-01T00:00:00Z",
          },
          {
            id: "t2",
            title: "loose",
            running: false,
            last_active_at: "2026-01-01T00:00:00Z",
          },
        ]}
        running={[{ thread_id: "t3", title: "gamma", action: "reading" }]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={onNewChat}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByRole("heading", { name: "notes" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Open loose" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "New chat in Recent" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "New chat in In progress" })).not.toBeInTheDocument()
    const startNotes = screen.getByRole("button", { name: "New chat in notes" })
    expect(startNotes.textContent?.trim()).toBe("")
    fireEvent.click(startNotes)
    expect(onNewChat).toHaveBeenCalledWith("p2")
    fireEvent.click(screen.getByRole("button", { name: "New chat in work" }))
    expect(onNewChat).toHaveBeenLastCalledWith("p1")

    fireEvent.change(screen.getByLabelText("Search conversations"), {
      target: { value: "alpha" },
    })
    expect(screen.getByRole("button", { name: "New chat in work" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "New chat in notes" })).not.toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: "Recent" })).not.toBeInTheDocument()
  })

  it("says the inbox is empty instead of showing a blank screen", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[]}
        running={[]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByText("Nothing running")).toBeInTheDocument()
    expect(screen.getByTestId("new-chat")).toBeInTheDocument()
  })

  // Search runs on the roster the phone already holds; a keystroke over a
  // relay would land after the next one.
  it("filters the rows to what was typed and says so when nothing matches", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[
          {
            id: "t1",
            title: "alpha",
            running: false,
            last_active_at: "2026-01-01T00:00:00Z",
          },
          {
            id: "t2",
            title: "beta",
            running: false,
            last_active_at: "2026-01-01T00:00:00Z",
          },
        ]}
        running={[{ thread_id: "t3", title: "gamma", action: "reading" }]}
        more
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    const box = screen.getByLabelText("Search conversations")
    fireEvent.change(box, { target: { value: "bet" } })
    expect(screen.getByRole("button", { name: "Open beta" })).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Open alpha" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Open gamma" })).not.toBeInTheDocument()
    // More pages the roster, not the filter; it would drop the query's rows.
    expect(screen.queryByRole("button", { name: "More" })).not.toBeInTheDocument()

    fireEvent.change(box, { target: { value: "no such row" } })
    expect(screen.getByText("No conversation matches that.")).toBeInTheDocument()
    expect(screen.queryByText("Nothing running")).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "Clear search" }))
    expect(screen.getByRole("button", { name: "Open alpha" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Open gamma" })).toBeInTheDocument()
  })

  // A parked wait is never also a Recents row, so the roster row is the only
  // place its line and its age can come from. A wait armed two hours ago
  // that reads as a bare badge says nothing about what it is waiting on.
  it("dates a parked wait and says what it is waiting on", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[]}
        running={[
          {
            thread_id: "t1",
            title: "thread 1",
            waiting: true,
            action: "checking again later",
            last_active_at: new Date(Date.now() - 2 * 3_600_000).toISOString(),
          },
        ]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByText("checking again later")).toBeInTheDocument()
    expect(screen.getByText("2h ago")).toBeInTheDocument()
  })

  // Running is happening now; a date on it would only be the date of the
  // frame that painted it.
  it("does not date a row that is running", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[]}
        running={[
          {
            thread_id: "t1",
            title: "thread 1",
            turn_id: "tu",
            action: "reading a file",
            last_active_at: new Date(Date.now() - 2 * 3_600_000).toISOString(),
          },
        ]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByText("Running")).toBeInTheDocument()
    expect(screen.queryByText("2h ago")).not.toBeInTheDocument()
  })

  it("reloads the roster when the list is pulled down", async () => {
    const onRefresh = vi.fn().mockResolvedValue(undefined)
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[]}
        running={[]}
        more={false}
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
        onRefresh={onRefresh}
      />,
    )
    const scroller = screen.getByTestId("inbox-scroller")
    Object.defineProperty(scroller, "scrollTop", { value: 0, configurable: true })
    fireEvent.touchStart(scroller, { touches: [{ clientY: 0 }] })
    fireEvent.touchMove(scroller, { touches: [{ clientY: 200 }] })
    await act(async () => {
      fireEvent.touchEnd(scroller)
    })
    expect(onRefresh).toHaveBeenCalled()
  })

  it("keeps five project recents when in-progress is a separate roster", () => {
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[{ id: "p", name: "work" }]}
        threads={Array.from({ length: 5 }, (_, i) => ({
          id: "idle" + i,
          title: "idle " + i,
          project_id: "p",
          running: false,
          last_active_at: "2026-01-01T00:00:00Z",
        }))}
        running={Array.from({ length: 4 }, (_, i) => ({
          thread_id: "live" + i,
          title: "live " + i,
          action: "read",
        }))}
        more
        onOpen={vi.fn()}
        onMore={vi.fn()}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByText("In progress")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "work" })).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: /^Open live / })).toHaveLength(4)
    expect(screen.getAllByRole("button", { name: /^Open idle / })).toHaveLength(5)
  })

  // In progress is a roster, not a replacement for the folder. Folding the
  // project hides its rows and still leaves the live copy, and Back must
  // not forget the fold.
  it("keeps a live conversation under its project and folds that project", () => {
    const props = {
      ...chrome(),
      path: "relay" as const,
      projects: [{ id: "p", name: "work" }],
      threads: [
        {
          id: "idle",
          title: "idle",
          project_id: "p",
          running: false,
          last_active_at: "2026-01-01T00:00:00Z",
        },
      ],
      running: [{ thread_id: "hot", title: "hot", project_id: "p", action: "reading" }],
      more: false,
      onOpen: vi.fn(),
      onMore: vi.fn(),
      onNewChat: vi.fn(),
      onUnlink: vi.fn(),
    }
    const first = render(<HomeScreen {...props} />)
    expect(screen.getAllByRole("button", { name: "Open hot" })).toHaveLength(2)
    expect(screen.getAllByText("Running")).toHaveLength(2)
    expect(screen.getByRole("button", { name: "Open idle" })).toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "work" }))
    expect(screen.getByRole("button", { name: "work" })).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByRole("button", { name: "Open idle" })).not.toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Open hot" })).toHaveLength(1)
    expect(screen.getByRole("button", { name: "New chat in work" })).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText("Search conversations"), { target: { value: "idle" } })
    expect(screen.getByRole("button", { name: "Open idle" })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Search conversations"), { target: { value: "" } })
    expect(screen.queryByRole("button", { name: "Open idle" })).not.toBeInTheDocument()

    first.unmount()
    render(<HomeScreen {...props} />)
    expect(screen.getByRole("button", { name: "work" })).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByRole("button", { name: "Open idle" })).not.toBeInTheDocument()

    fireEvent.click(screen.getByRole("button", { name: "work" }))
    expect(screen.getAllByRole("button", { name: "Open hot" })).toHaveLength(2)
  })

  it("says it is loading more and will not take a second tap", () => {
    const onMore = vi.fn()
    render(
      <HomeScreen
        {...chrome()}
        path="relay"
        projects={[]}
        threads={[]}
        running={[]}
        more
        loadingMore
        onOpen={vi.fn()}
        onMore={onMore}
        onNewChat={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    const button = screen.getByRole("button", { name: "Loading more" })
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute("aria-busy", "true")
    fireEvent.click(button)
    expect(onMore).not.toHaveBeenCalled()
  })
})
