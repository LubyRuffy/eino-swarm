import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { QueuedSteers } from "./queued-steers"
import type { Block } from "@/lib/transcript"

function steer(text: string, seq: number): Block {
  return {
    id: `steer-${seq}`,
    kind: "steer",
    agentId: "manager",
    text,
    turnId: "t1",
    seq,
    at: new Date().toISOString(),
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

  it("renders nothing when the inbox is empty", () => {
    const { container } = render(
      <QueuedSteers blocks={[]} onPreempt={() => {}} onRetract={() => {}} />,
    )
    expect(container).toBeEmptyDOMElement()
  })
})
