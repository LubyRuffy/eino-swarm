import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { TextFilePreview } from "./text-file-preview"
import { t } from "@/lib/i18n"

describe("text output preview", () => {
  it.each(["reading.md", "source.go", "plain.txt", "large.md"])("adjusts the complete %s content within readable limits and resets its size", (path) => {
    const text = path === "large.md" ? "Content\n".repeat(40000) : "# Heading\n\nText\n"
    render(<TextFilePreview path={path} text={text} />)
    const content = screen.getByTestId("phone-text-content")
    const smaller = screen.getByRole("button", { name: t("files.textSmaller") })
    const larger = screen.getByRole("button", { name: t("files.textLarger") })
    const reset = screen.getByRole("button", { name: t("files.textReset") })
    expect(content).toHaveStyle({ fontSize: "14px" })
    expect(reset).toBeDisabled()
    fireEvent.click(smaller)
    expect(content).toHaveStyle({ fontSize: "12px" })
    expect(smaller).toBeDisabled()
    for (let n = 0; n < 12; n++) fireEvent.click(larger)
    expect(content).toHaveStyle({ fontSize: "28px" })
    expect(larger).toBeDisabled()
    expect(screen.getByLabelText(t("files.textSize"))).toHaveTextContent("28")
    fireEvent.click(reset)
    expect(content).toHaveStyle({ fontSize: "14px" })
    if (path !== "reading.md") expect(content.querySelector("pre")?.textContent).toBe(text)
    else expect(screen.getByRole("heading", { name: "Heading" })).toBeInTheDocument()
  })

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
    expect(screen.getByTestId("phone-text-content").querySelector("svg, script")).toBeNull()
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
