import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { QuotedMessageBody } from "./quoted-message"
import { formatQuotedMessage } from "@/lib/quote"

describe("QuotedMessageBody", () => {
  it("paints a plain instruction as-is", () => {
    render(<QuotedMessageBody text="do this" />)
    expect(screen.getByText("do this")).toBeInTheDocument()
    expect(screen.queryByTestId("quoted-message")).toBeNull()
    expect(screen.queryByTestId("quote-snippet")).toBeNull()
  })

  it("splits tagged highlights from the request so the bubble is not one dump", () => {
    render(
      <QuotedMessageBody text={formatQuotedMessage(["alpha beta"], "do this")} />,
    )
    const chip = screen.getByTestId("quote-snippet")
    expect(chip).toHaveTextContent("Selected text:")
    expect(chip).toHaveTextContent("alpha beta")
    expect(screen.getByText("do this")).toBeInTheDocument()
    expect(screen.queryByText(/<selected_text>/)).not.toBeInTheDocument()
    expect(screen.queryByText(/<user_request>/)).not.toBeInTheDocument()
  })

  it("still splits a legacy Selected text prefix", () => {
    render(<QuotedMessageBody text={"Selected text:\nalpha\n\ndo this"} />)
    expect(screen.getByTestId("quote-snippet")).toHaveTextContent("alpha")
    expect(screen.getByText("do this")).toBeInTheDocument()
  })

  // A highlight sized to the raw string stretches the composer and the
  // bubble. Three wrapped lines is the cap; the rest stays in the DOM
  // for the model, and the chip shows an ellipsis.
  it("wraps a long highlight to three lines instead of stretching the row", () => {
    const passage = "alpha beta ".repeat(40).trim()
    render(<QuotedMessageBody text={formatQuotedMessage([passage], "do this")} />)
    const chip = screen.getByTestId("quote-snippet")
    const text = screen.getByTestId("quote-snippet-text")
    expect(chip.className).toMatch(/\bmin-w-0\b/)
    expect(chip.className).toMatch(/\bmax-w-full\b/)
    expect(chip.className).not.toMatch(/\bw-fit\b/)
    expect(text).toHaveClass("line-clamp-3")
    expect(text.className).toMatch(/\bbreak-words\b/)
    expect(text.className).toMatch(/\bwhitespace-pre-wrap\b/)
    expect(text.className).not.toMatch(/\btruncate\b/)
    expect(text).toHaveTextContent(passage)
    expect(chip.querySelector("button")).toBeNull()
  })
})
