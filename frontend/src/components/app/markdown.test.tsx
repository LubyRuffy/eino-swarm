import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { MemoMarkdown } from "./markdown"

describe("MemoMarkdown", () => {
  it("renders a streaming heading as a heading", () => {
    render(<MemoMarkdown text={"## Result\n\nstill writing"} streaming />)
    expect(screen.getByRole("heading", { name: "Result" })).toBeInTheDocument()
    expect(screen.queryByText("## Result")).not.toBeInTheDocument()
  })

  it("renders trailing bold while it is still open", () => {
    render(<MemoMarkdown text="see **alpha" streaming />)
    const strong = screen.getByText("alpha")
    expect(strong.closest("strong")).toBeTruthy()
  })

  it("does not invent closers on a finished answer", () => {
    render(<MemoMarkdown text="see **alpha" />)
    expect(document.querySelector("strong")).toBeNull()
    expect(screen.getByText(/see \*\*alpha/)).toBeInTheDocument()
  })

  it("sends an http link out of this window, not through it", () => {
    render(<MemoMarkdown text="see [docs](https://example.invalid/docs)" />)
    const link = screen.getByRole("link", { name: "docs" })
    expect(link).toHaveAttribute("href", "https://example.invalid/docs")
    expect(link).toHaveAttribute("target", "_blank")
    expect(link).toHaveAttribute("rel", "noopener noreferrer")
  })

  it("keeps a fragment link inside the page", () => {
    render(<MemoMarkdown text="jump [here](#fn-1)" />)
    const link = screen.getByRole("link", { name: "here" })
    expect(link).toHaveAttribute("href", "#fn-1")
    expect(link).not.toHaveAttribute("target")
  })
})
