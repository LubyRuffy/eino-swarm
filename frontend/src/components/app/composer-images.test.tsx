import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ComposerImages } from "./composer-images"
import type { PasteImage } from "@/lib/paste-image"

const sample: PasteImage[] = [
  {
    id: "paste_1",
    name: "clip.png",
    mime: "image/png",
    file: new File([new Uint8Array([1])], "clip.png", { type: "image/png" }),
    previewUrl: "blob:preview",
  },
]

describe("ComposerImages", () => {
  it("shows a thumbnail that can be removed", () => {
    const onRemove = vi.fn()
    render(<ComposerImages images={sample} onRemove={onRemove} />)
    expect(screen.getByAltText("clip.png")).toHaveAttribute("src", "blob:preview")
    fireEvent.click(screen.getByRole("button", { name: "Remove clip.png" }))
    expect(onRemove).toHaveBeenCalledWith("paste_1")
  })

  it("renders nothing when the composer has no paste", () => {
    const { container } = render(<ComposerImages images={[]} onRemove={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })
})
