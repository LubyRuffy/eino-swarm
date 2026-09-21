import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { MemoMarkdown } from "./markdown"

vi.mock("@/components/app/transcript-chart", async () => {
  const { useId } = await import("react")
  return {
    TranscriptChart: ({ spec }: { spec: { title: string } }) => {
      const id = useId()
      return (
        <figure
          data-testid="transcript-chart"
          data-instance={id}
          aria-label={spec.title}
        />
      )
    },
  }
})

describe("MemoMarkdown", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    Reflect.deleteProperty(document, "execCommand")
  })
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

  it("paints a closed chart fence instead of dumping the JSON", async () => {
    const text = [
      "see",
      "",
      "```chart",
      JSON.stringify({
        type: "bar",
        title: "Counts",
        x: "item",
        y: "n",
        data: [
          { item: "a", n: 1 },
          { item: "b", n: 3 },
        ],
      }),
      "```",
    ].join("\n")
    render(<MemoMarkdown text={text} />)
    expect(await screen.findByTestId("transcript-chart")).toBeInTheDocument()
    expect(screen.getByRole("figure", { name: "Counts" })).toBeInTheDocument()
    expect(screen.queryByText(/"type": "bar"/)).not.toBeInTheDocument()
  })

  it("keeps a closed chart mounted while later tokens arrive", async () => {
    const fence = [
      "see",
      "",
      "```chart",
      JSON.stringify({
        type: "bar",
        title: "Counts",
        x: "item",
        y: "n",
        data: [
          { item: "a", n: 1 },
          { item: "b", n: 3 },
        ],
      }),
      "```",
    ].join("\n")
    const { rerender } = render(<MemoMarkdown text={fence} streaming />)
    const chart = await screen.findByTestId("transcript-chart")
    const instance = chart.getAttribute("data-instance")
    expect(instance).toBeTruthy()

    rerender(<MemoMarkdown text={fence + "\n\nand then more prose"} streaming />)
    expect(screen.getByTestId("transcript-chart")).toHaveAttribute(
      "data-instance",
      instance,
    )
    expect(screen.getByText(/and then more prose/)).toBeInTheDocument()
  })

  it("does not treat a json fence as a chart", () => {
    const text = "```json\n{\"type\":\"bar\"}\n```"
    render(<MemoMarkdown text={text} />)
    expect(screen.queryByTestId("transcript-chart")).not.toBeInTheDocument()
    expect(screen.getByTestId("markdown-code").textContent).toContain('"type":"bar"')
  })

  it("highlights a language-tagged fence and copies the original body", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    const src = "func Len(s string) int { return len(s) }"
    render(<MemoMarkdown text={"```go\n" + src + "\n```"} />)
    expect(screen.getByText("func")).toHaveClass("text-syntax-keyword")
    expect(screen.getByText("Len")).toHaveClass("text-syntax-command")
    expect(screen.getByTestId("markdown-code").textContent).toContain(src)
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(src))
  })

  it("maps a cpp fence onto the C highlighter", () => {
    render(<MemoMarkdown text={"```cpp\nint x = 1;\n```"} />)
    expect(screen.getByText("int")).toHaveClass("text-syntax-keyword")
    expect(screen.getByText("cpp")).toBeInTheDocument()
  })

  it("leaves an unlabeled fence uncoloured and still copyable", () => {
    render(<MemoMarkdown text={"```\nnot a language\n```"} />)
    expect(document.querySelector(".text-syntax-keyword")).toBeNull()
    expect(screen.getByRole("button", { name: "Copy code" })).toBeInTheDocument()
    expect(screen.getByTestId("markdown-code").textContent).toContain("not a language")
  })

  it("still copies a fence when the clipboard API refuses", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"))
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    Object.defineProperty(document, "execCommand", {
      configurable: true,
      writable: true,
      value: vi.fn(() => true),
    })
    render(<MemoMarkdown text={"```\nalpha\n```"} />)
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }))
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Copy code" })).toHaveAttribute("title", "Copied"),
    )
    expect(writeText).not.toHaveBeenCalled()
  })

  it("renders inline and display math as KaTeX, not as dollar signs", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    render(<MemoMarkdown text={"see $n$ and\n\n$$\n a + b \n$$"} />)
    expect(screen.queryByText(/\$n\$/)).not.toBeInTheDocument()
    await waitFor(() => expect(document.querySelectorAll(".katex").length).toBeGreaterThanOrEqual(2))
    expect(screen.getByTestId("markdown-math")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Copy formula" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(" a + b "))
  })

  it("does not treat a dollar path as math", () => {
    render(<MemoMarkdown text="see $HOME" />)
    expect(document.querySelector(".katex")).toBeNull()
    expect(screen.getByText(/\$HOME/)).toBeInTheDocument()
  })

  it("renders a math fence as a formula, not as source", async () => {
    render(<MemoMarkdown text={"```math\n a + b \n```"} />)
    expect(await screen.findByTestId("markdown-math")).toBeInTheDocument()
    await waitFor(() => expect(document.querySelector(".katex")).toBeTruthy())
    expect(screen.queryByTestId("markdown-code")).not.toBeInTheDocument()
  })

  it("closes a live display-math fence so KaTeX can paint mid-stream", async () => {
    render(<MemoMarkdown text={"$$\n a + b"} streaming />)
    await waitFor(() => expect(document.querySelector(".katex")).toBeTruthy())
  })

  it("holds a live unclosed chart fence as a placeholder, not as raw JSON", () => {
    render(
      <MemoMarkdown text={'```chart\n{"type":"bar","x":"item"'} streaming />,
    )
    expect(screen.getByTestId("transcript-chart-pending")).toBeInTheDocument()
    expect(screen.queryByTestId("transcript-chart")).not.toBeInTheDocument()
  })

  it("leaves a finished invalid chart fence as code so the body is still readable", () => {
    render(<MemoMarkdown text={'```chart\n{"type":"nope"}\n```'} />)
    expect(screen.queryByTestId("transcript-chart")).not.toBeInTheDocument()
    expect(screen.queryByTestId("transcript-chart-pending")).not.toBeInTheDocument()
    expect(screen.getByText(/"type":"nope"/)).toBeInTheDocument()
  })

  // A long unbreakable cell used to set the column's min-content and paint
  // under the side panel. The scrollport is the cap; the cell still renders.
  it("parks a gfm table in a scrollport so a long cell cannot widen the column", () => {
    const cell = "x".repeat(80)
    render(<MemoMarkdown text={`| k | v |\n| --- | --- |\n| a | ${cell} |`} />)
    const table = screen.getByRole("table")
    expect(table.parentElement).toHaveAttribute("data-testid", "markdown-table")
    expect(table.parentElement).toHaveClass("md-table")
    expect(table.parentElement?.className).toMatch(/\boverflow-x-auto\b/)
    expect(screen.getByText(cell)).toBeInTheDocument()
    expect(document.body.textContent).not.toMatch(/fofa|body=|FatalError/i)
  })
})
