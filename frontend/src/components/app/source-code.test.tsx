import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { SourceListing } from "./source-code"

describe("SourceListing", () => {
  it("paints keywords from the suffix onto tokens, not the whole line", () => {
    render(
      <SourceListing
        path="main.go"
        body={'package main\nfunc Hello() { return "ok" }'}
        lines={[
          { n: 1, text: "package main" },
          { n: 2, text: 'func Hello() { return "ok" }' },
        ]}
      />,
    )
    expect(screen.getByText("package")).toHaveClass("text-syntax-keyword")
    expect(screen.getByText("main")).toHaveClass("text-syntax-command")
    expect(screen.getByText("Hello")).toHaveClass("text-syntax-command")
    expect(screen.getByText(`"ok"`)).toHaveClass("text-syntax-string")
    expect(screen.getByText("1")).toBeInTheDocument()
    expect(screen.getByText("2")).toBeInTheDocument()
  })

  it("keeps a blank line so a gap in the file is still a gap", () => {
    render(
      <SourceListing
        path="main.go"
        body={"package main\n\nfunc Hello() {}"}
        lines={[
          { n: 1, text: "package main" },
          { n: 2, text: "" },
          { n: 3, text: "func Hello() {}" },
        ]}
      />,
    )
    expect(screen.getByText("2")).toBeInTheDocument()
    expect(screen.getByText("Hello")).toHaveClass("text-syntax-command")
  })
})
