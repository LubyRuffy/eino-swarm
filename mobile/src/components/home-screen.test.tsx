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
    expect(screen.getByText("In progress")).toBeInTheDocument()
    expect(screen.getByText("thread 4")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "More" })).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("New message"), {
      target: { value: "hello" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Start" }))
    expect(onStart).toHaveBeenCalledWith("hello", "")
    fireEvent.click(screen.getByText("thread 2"))
    expect(onOpen).toHaveBeenCalledWith("t2")
  })
})
