import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
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

  it("opens the selected original image and restores focus after closing", async () => {
    const user = userEvent.setup()
    render(
      <InputThumbs threadId="th_1" images={[
        { id: "img_ab", name: "first.png", mime: "image/png" },
        { id: "img_cd", name: "second.jpg", mime: "image/jpeg" },
      ]} />,
    )
    const trigger = screen.getByRole("button", { name: "Enlarge second.jpg" })
    await user.click(trigger)
    const dialog = screen.getByRole("dialog", { name: "second.jpg" })
    const image = within(dialog).getByAltText("second.jpg")
    expect(image).toHaveAttribute("src", "/api/threads/th_1/input-images/img_cd")
    expect(within(dialog).queryByAltText("first.png")).toBeNull()
    expect(within(dialog).getByRole("status")).toHaveTextContent("Loading image")
    fireEvent.load(image)
    expect(within(dialog).queryByRole("status")).toBeNull()
    expect(image).toBeVisible()
    await user.click(within(dialog).getByRole("button", { name: "Close image preview" }))
    expect(screen.queryByRole("dialog")).toBeNull()
    await waitFor(() => expect(trigger).toHaveFocus())
  })

  it("opens from the keyboard, closes with Escape, and starts fresh on reopen", async () => {
    const user = userEvent.setup()
    render(<InputThumbs threadId="th_1" images={[{ id: "img_ab", mime: "image/png" }]} />)
    const trigger = screen.getByRole("button", { name: "Enlarge Pasted image" })
    trigger.focus()
    await user.keyboard("{Enter}")
    const dialog = screen.getByRole("dialog", { name: "Pasted image" })
    fireEvent.error(within(dialog).getByAltText("Pasted image"))
    expect(within(dialog).getByRole("alert")).toHaveTextContent("Could not load image")
    await user.keyboard("{Escape}")
    expect(screen.queryByRole("dialog")).toBeNull()
    await waitFor(() => expect(trigger).toHaveFocus())
    await user.keyboard(" ")
    expect(screen.getByRole("status")).toHaveTextContent("Loading image")
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("dismisses an old preview when its conversation changes or image disappears", async () => {
    const user = userEvent.setup()
    const images = [{ id: "img_ab", name: "clip.png", mime: "image/png" }]
    const view = render(<InputThumbs threadId="th_1" images={images} />)
    await user.click(screen.getByRole("button", { name: "Enlarge clip.png" }))
    view.rerender(<InputThumbs threadId="th_2" images={images} />)
    expect(screen.queryByRole("dialog")).toBeNull()
    await user.click(screen.getByRole("button", { name: "Enlarge clip.png" }))
    expect(within(screen.getByRole("dialog")).getByAltText("clip.png"))
      .toHaveAttribute("src", "/api/threads/th_2/input-images/img_ab")
    view.rerender(<InputThumbs threadId="th_2" images={[]} />)
    expect(screen.queryByRole("dialog")).toBeNull()
  })
})
