import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { PhoneMarkdown } from "./markdown"

describe("PhoneMarkdown", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
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

  it("renders a math fence as a formula", () => {
    render(<PhoneMarkdown text={"```math\n a + b \n```"} />)
    expect(screen.getByTestId("markdown-math")).toBeInTheDocument()
    expect(document.querySelector(".katex")).toBeTruthy()
    expect(screen.queryByTestId("markdown-code")).not.toBeInTheDocument()
  })
})
