import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { QueueTray } from "./queue-tray"
import type { Followup } from "@/lib/types"

function item(id: string, text: string, seq = 1): Followup {
  return {
    id,
    thread_id: "th_1",
    seq,
    text,
    created_at: "2026-09-16T00:00:00Z",
  }
}

describe("QueueTray", () => {
  it("hides when nothing is waiting", () => {
    const { container } = render(
      <QueueTray
        items={[]}
        onSteer={vi.fn()}
        onDelete={vi.fn()}
        onClear={vi.fn()}
        onEdit={vi.fn()}
      />,
    )
    expect(container).toBeEmptyDOMElement()
  })

  it("names the count and steers or drops a row", () => {
    const onSteer = vi.fn()
    const onDelete = vi.fn()
    const onClear = vi.fn()
    render(
      <QueueTray
        items={[item("fu_1", "after this"), item("fu_2", "then that")]}
        onSteer={onSteer}
        onDelete={onDelete}
        onClear={onClear}
        onEdit={vi.fn()}
      />,
    )
    expect(screen.getByTestId("followup-queue")).toHaveTextContent("2 Queued")
    fireEvent.click(screen.getByRole("button", { name: "Steer: after this" }))
    expect(onSteer).toHaveBeenCalledWith("fu_1")
    fireEvent.click(
      screen.getByRole("button", { name: "Remove queued message: then that" }),
    )
    expect(onDelete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Remove" }))
    expect(onDelete).toHaveBeenCalledWith("fu_2")
    fireEvent.click(screen.getByRole("button", { name: "Clear queue" }))
    expect(onClear).not.toHaveBeenCalled()
    fireEvent.click(
      within(screen.getByRole("dialog")).getByRole("button", { name: "Clear queue" }),
    )
    expect(onClear).toHaveBeenCalled()
  })

  it("pulls a row into the composer instead of editing it in the tray", () => {
    const onEdit = vi.fn()
    const first = item("fu_1", "after this", 1)
    render(
      <QueueTray
        items={[first, item("fu_2", "then that", 2)]}
        onSteer={vi.fn()}
        onDelete={vi.fn()}
        onClear={vi.fn()}
        onEdit={onEdit}
      />,
    )
    fireEvent.click(
      screen.getByRole("button", { name: "Edit queued message: after this" }),
    )
    expect(onEdit).toHaveBeenCalledWith(first)
    expect(screen.queryByRole("textbox")).toBeNull()
  })
})
