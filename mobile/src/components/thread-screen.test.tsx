import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ThreadScreen } from "./thread-screen"

describe("ThreadScreen", () => {
  it("maps send and stop onto the live thread", () => {
    const onSend = vi.fn()
    const onStop = vi.fn()
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          running: { thread_id: "t1", title: "live", action: "write" },
          turns: [{ id: "u1", status: "completed", text: "done bit" }],
        }}
        onBack={vi.fn()}
        onSend={onSend}
        onStop={onStop}
        onAnswer={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Stop" }))
    expect(onStop).toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "keep going" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Follow-up" }))
    expect(onSend).toHaveBeenCalledWith("keep going")
  })
})
