import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { QueuedSteers } from "./queued-steers"
import type { Block } from "@/lib/transcript"
import { formatQuotedMessage } from "@/lib/quote"

function steer(text: string, seq: number, images?: Block["images"]): Block {
  return {
    id: `steer-${seq}`,
    kind: "steer",
    agentId: "manager",
    text,
    turnId: "t1",
    seq,
    at: new Date().toISOString(),
    images,
  }
}

describe("queued steering pin", () => {
  it("interrupts the tool for every unread bubble and retracts one by seq", () => {
    const onPreempt = vi.fn()
    const onRetract = vi.fn()
    render(
      <QueuedSteers
        blocks={[steer("first nudge", 3), steer("first nudge", 4)]}
        onPreempt={onPreempt}
        onRetract={onRetract}
        onEdit={vi.fn()}
      />,
    )
    const pin = screen.getByTestId("queued-steers")
    expect(pin).toHaveTextContent("first nudge")
    fireEvent.click(screen.getByRole("button", { name: "Abort the current tool and inject queued steering" }))
    expect(onPreempt).toHaveBeenCalledTimes(1)
    const deletes = screen.getAllByRole("button", { name: "Remove this unread steering" })
    expect(deletes).toHaveLength(2)
    fireEvent.click(deletes[0])
    expect(onRetract).toHaveBeenCalledWith(3)
    fireEvent.click(deletes[1])
    expect(onRetract).toHaveBeenCalledWith(4)
  })

  it("splits a quoted steer so the highlight is not the same dump as the nudge", () => {
    render(
      <QueuedSteers
        blocks={[steer(formatQuotedMessage(["alpha beta"], "do this"), 3)]}
        onPreempt={() => {}}
        onRetract={() => {}}
        onEdit={() => {}}
      />,
    )
    expect(screen.getByTestId("quote-snippet")).toHaveTextContent("alpha beta")
    expect(screen.getByText("do this")).toBeInTheDocument()
    expect(screen.queryByText(/<selected_text>/)).not.toBeInTheDocument()
  })

  it("pulls the caption into the composer instead of editing it on the pin", () => {
    const onEdit = vi.fn()
    const text = formatQuotedMessage(["alpha beta"], "do this")
    render(
      <QueuedSteers
        blocks={[steer(text, 3)]}
        onPreempt={() => {}}
        onRetract={() => {}}
        onEdit={onEdit}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Edit this unread steering" }))
    expect(onEdit).toHaveBeenCalledWith(text, 3)
    expect(screen.queryByRole("textbox")).toBeNull()
  })

  it("does not offer edit when the steer is only an image", () => {
    render(
      <QueuedSteers
        blocks={[
          steer("", 3, [{ id: "img_1", name: "clip.png", mime: "image/png" }]),
        ]}
        onPreempt={() => {}}
        onRetract={() => {}}
        onEdit={vi.fn()}
      />,
    )
    expect(screen.queryByRole("button", { name: "Edit this unread steering" })).toBeNull()
    expect(screen.getByRole("button", { name: "Remove this unread steering" })).toBeInTheDocument()
  })

  it("renders nothing when the inbox is empty", () => {
    const { container } = render(
      <QueuedSteers blocks={[]} onPreempt={() => {}} onRetract={() => {}} />,
    )
    expect(container).toBeEmptyDOMElement()
  })
})
