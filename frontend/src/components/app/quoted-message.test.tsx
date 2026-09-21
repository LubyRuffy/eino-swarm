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
})
