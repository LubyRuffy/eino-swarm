import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ComposerAttachments } from "./composer-attachments"

const notes = new File([new Uint8Array([1, 2, 3])], "notes.txt", {
  type: "text/plain",
})

describe("ComposerAttachments", () => {
  it("shows a removable chip for each pending file", () => {
    const onRemove = vi.fn()
    render(<ComposerAttachments files={[notes]} onRemove={onRemove} />)
    expect(screen.getByTestId("composer-attachments")).toHaveTextContent("notes.txt")
    fireEvent.click(screen.getByRole("button", { name: "Remove notes.txt" }))
    expect(onRemove).toHaveBeenCalledWith(0)
  })

  it("marks the chips busy while they are being added to the workspace", () => {
    render(<ComposerAttachments files={[notes]} uploading onRemove={vi.fn()} />)
    expect(screen.getByTestId("composer-attachments")).toHaveAttribute("aria-busy", "true")
  })

  it("renders nothing when nothing is attached", () => {
    const { container } = render(
      <ComposerAttachments files={[]} onRemove={vi.fn()} />,
    )
    expect(container).toBeEmptyDOMElement()
  })
})
