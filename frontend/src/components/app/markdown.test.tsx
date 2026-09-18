import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { MemoMarkdown } from "./markdown"

vi.mock("@/components/app/transcript-chart", () => ({
  TranscriptChart: ({ spec }: { spec: { title: string } }) => (
    <figure data-testid="transcript-chart" aria-label={spec.title} />
  ),
}))

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

  it("does not treat a json fence as a chart", () => {
    const text = "```json\n{\"type\":\"bar\"}\n```"
    render(<MemoMarkdown text={text} />)
    expect(screen.queryByTestId("transcript-chart")).not.toBeInTheDocument()
    expect(screen.getByText(/"type":"bar"/)).toBeInTheDocument()
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
})
