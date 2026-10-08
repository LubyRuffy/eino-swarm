import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { TextFilePreview } from "./text-file-preview"

describe("text output preview", () => {
  it.each(["summary.md", "SUMMARY.MARKDOWN", "notes.mdx"])("renders %s without evaluating HTML or MDX", (path) => {
    render(<TextFilePreview path={path} text={'# Heading\n\n<script>bad()</script>\n\n<Component />\n\n```go\npackage main\n```'} />)
    expect(screen.getByRole("heading", { name: "Heading" })).toBeInTheDocument()
    expect(document.querySelector("script, component")).toBeNull()
    expect(document.querySelector("pre")?.textContent).toBe("package main")
  })

  it.each(["source.html", "image.svg"])("keeps %s as escaped source", (path) => {
    const text = '<svg onload="bad()"><script>bad()</script></svg>'
    const { container } = render(<TextFilePreview path={path} text={text} />)
    expect(container.querySelector("pre")?.textContent).toBe(text)
    expect(container.querySelector("svg, script")).toBeNull()
    expect(container.querySelector(".text-syntax-string")).not.toBeNull()
  })

  it.each(["large.md", "large.go"])("falls back to the complete text for %s", (path) => {
    const text = '# Heading\n' + 'const x = "value"\n'.repeat(18000)
    const { container } = render(<TextFilePreview path={path} text={text} />)
    expect(container.querySelector("pre")?.textContent).toBe(text)
    expect(container.querySelector(".text-syntax-keyword, h1")).toBeNull()
  })

  it("handles an empty file and a long line without losing characters", () => {
    const { container, rerender } = render(<TextFilePreview path="empty.txt" text="" />)
    expect(container.querySelector("pre")?.textContent).toBe("")
    const text = 'const x = "' + "x".repeat(4097) + '"'
    rerender(<TextFilePreview path="long.go" text={text} />)
    expect(container.querySelector("pre")?.textContent).toBe(text)
    expect(container.querySelector(".text-syntax-keyword")).toBeNull()
  })
})
