import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PhoneMarkdown } from "./markdown"

describe("PhoneMarkdown", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    Reflect.deleteProperty(document, "execCommand")
  })

  it("copies a fenced body and renders math", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    const src = "func Len(s string) int { return len(s) }"
    render(<PhoneMarkdown text={"see $n$\n\n```go\n" + src + "\n```"} />)
    expect(document.querySelector(".katex")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(src))
  })

  it("still copies a fence when the clipboard API refuses", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"))
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      writable: true,
      value: vi.fn(() => true),
    })
    render(<PhoneMarkdown text={"```\nalpha\n```"} />)
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }))
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Copy code" })).toHaveAttribute("title", "Copied"),
    )
    expect(writeText).not.toHaveBeenCalled()
  })

  it("renders a math fence as a formula", () => {
    render(<PhoneMarkdown text={"```math\n a + b \n```"} />)
    expect(screen.getByTestId("markdown-math")).toBeInTheDocument()
    expect(document.querySelector(".katex")).toBeTruthy()
    expect(screen.queryByTestId("markdown-code")).not.toBeInTheDocument()
  })

  it("renders emphasis, a gfm table, and does not keep the markers", () => {
    render(
      <PhoneMarkdown
        text={"see **alpha**\n\n| col |\n| --- |\n| val |"}
      />,
    )
    const strong = screen.getByText("alpha")
    expect(strong.closest("strong")).toBeTruthy()
    expect(screen.queryByText(/\*\*alpha\*\*/)).not.toBeInTheDocument()
    expect(screen.getByRole("table")).toBeInTheDocument()
    expect(screen.getByText("val")).toBeInTheDocument()
  })

  it("sends an http link out of the webview", () => {
    render(<PhoneMarkdown text="see [docs](https://example.invalid/docs)" />)
    const link = screen.getByRole("link", { name: "docs" })
    expect(link).toHaveAttribute("href", "https://example.invalid/docs")
    expect(link).toHaveAttribute("target", "_blank")
  })

  it("does not turn a filesystem path into a navigable link", () => {
    render(<PhoneMarkdown text={"open [notes](/tmp/notes.md)"} />)
    expect(screen.queryByRole("link")).not.toBeInTheDocument()
    expect(screen.getByText("notes")).toBeInTheDocument()
  })
})
