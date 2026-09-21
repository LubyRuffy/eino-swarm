import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { HomeScreen } from "./home-screen"

describe("HomeScreen", () => {
  it("lists five threads, in-progress, and a start field", () => {
    const onOpen = vi.fn()
    const onStart = vi.fn()
    render(
      <HomeScreen
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
})
