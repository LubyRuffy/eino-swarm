import { fireEvent, render, screen } from "@testing-library/react"
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
  it("lists five threads, in-progress, and a start field", () => {
    const onOpen = vi.fn()
    const onStart = vi.fn()
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
        onStart={onStart}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByLabelText("path=relay")).toBeInTheDocument()
    expect(screen.getByText("In progress")).toBeInTheDocument()
    expect(screen.getByText("thread 4")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "More" })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("New message"), {
      target: { value: "hello" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    expect(onStart).toHaveBeenCalledWith("hello", "")
    expect(screen.queryByText("New conversation")).not.toBeInTheDocument()
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
        onStart={vi.fn()}
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
        onStart={vi.fn()}
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
        onStart={vi.fn()}
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
        onStart={vi.fn()}
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
        onStart={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getAllByText("one thing changed")).toHaveLength(2)
    expect(screen.queryByText(/report_schedule/)).not.toBeInTheDocument()
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
        onStart={vi.fn()}
        onUnlink={vi.fn()}
      />,
    )
    expect(screen.getByText("In progress")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "work" })).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: /^Open live / })).toHaveLength(4)
    expect(screen.getAllByRole("button", { name: /^Open idle / })).toHaveLength(5)
  })
})
