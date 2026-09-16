import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { InputThumbs } from "./input-thumbs"

describe("InputThumbs", () => {
  it("points at the conversation's input-images URL", () => {
    render(
      <InputThumbs
        threadId="th_1"
        images={[{ id: "img_ab", name: "clip.png", mime: "image/png" }]}
      />,
    )
    const img = screen.getByAltText("clip.png")
    expect(img).toHaveAttribute(
      "src",
      "/api/threads/th_1/input-images/img_ab",
    )
  })

  it("renders nothing without a conversation or images", () => {
    const { container, rerender } = render(
      <InputThumbs threadId="th_1" images={[]} />,
    )
    expect(container).toBeEmptyDOMElement()
    rerender(<InputThumbs images={[{ id: "img_ab", mime: "image/png" }]} />)
    expect(container).toBeEmptyDOMElement()
  })
})
