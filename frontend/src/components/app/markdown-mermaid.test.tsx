import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { MemoMarkdown } from "./markdown"

const mermaid = vi.hoisted(() => ({
  initialize: vi.fn(),
  render: vi.fn(),
  mermaidAPI: { defaultConfig: { sequence: {}, class: {}, c4: {} } },
}))
vi.mock("mermaid", () => ({ default: mermaid }))

const SVG = '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 80"><text x="10" y="30">Start → Finish</text></svg>'
const SOURCE = "flowchart LR\n  A[Start] --> B[Finish]"
const fence = (source = SOURCE) => `\`\`\`mermaid\n${source}\n\`\`\``

describe("Mermaid markdown", () => {
  beforeEach(() => {
    mermaid.initialize.mockReset()
    mermaid.render.mockReset().mockResolvedValue({ svg: SVG })
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    document.documentElement.classList.remove("dark")
  })

  it("renders a Mermaid fence as an image and copies its original source", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    render(<MemoMarkdown text={fence()} />)
    const image = await screen.findByRole("img", { name: "Mermaid" })
    expect(decodeURIComponent(image.getAttribute("src")!.split(",")[1])).toContain("Start → Finish")
    expect(mermaid.render).toHaveBeenCalledWith(expect.stringMatching(/^mermaid-/), SOURCE, expect.any(HTMLElement))
    fireEvent.click(screen.getByRole("button", { name: "Copy code" }))
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(SOURCE))
    expect(screen.getByTestId("markdown-mermaid").querySelector("details code")?.textContent).toBe(SOURCE)
    expect(screen.getByText("Source")).toBeInTheDocument()
    expect(mermaid.initialize).toHaveBeenCalledWith(expect.objectContaining({
      securityLevel: "strict", htmlLabels: false, startOnLoad: false,
      suppressErrorRendering: true,
      secure: expect.arrayContaining(["securityLevel", "htmlLabels", "themeCSS", "fontFamily", "dompurifyConfig"]),
    }))
    expect(document.querySelector(".md-mermaid-measure")).toBeNull()
  })

  it("waits for a streamed fence to close and keeps its image while prose grows", async () => {
    const { rerender } = render(<MemoMarkdown text={`\`\`\`mermaid\n${SOURCE}`} streaming />)
    expect(screen.getByTestId("markdown-code").textContent).toContain(SOURCE)
    expect(mermaid.render).not.toHaveBeenCalled()
    rerender(<MemoMarkdown text={fence()} streaming />)
    const image = await screen.findByRole("img", { name: "Mermaid" })
    rerender(<MemoMarkdown text={fence() + "\n\nMore text"} streaming />)
    expect(screen.getByRole("img", { name: "Mermaid" })).toBe(image)
    expect(mermaid.render).toHaveBeenCalledTimes(1)
  })

  it("renders closed Mermaid fences inside Markdown containers", async () => {
    render(<MemoMarkdown text={fence().split("\n").map((line) => `> ${line}`).join("\n")} streaming />)
    expect(await screen.findByRole("img", { name: "Mermaid" })).toBeInTheDocument()
    expect(mermaid.render).toHaveBeenCalledTimes(1)
  })

  it("keeps invalid or undecodable diagrams copyable and removes measurement DOM", async () => {
    mermaid.render.mockRejectedValueOnce(new Error("syntax"))
    const { rerender } = render(<MemoMarkdown text={fence("flowchart LR\n broken[[")} />)
    await waitFor(() => expect(mermaid.render).toHaveBeenCalledTimes(1))
    await waitFor(() => expect(document.querySelector(".md-mermaid-measure")).toBeNull())
    expect(screen.getByTestId("markdown-code").textContent).toContain("broken[[")
    expect(screen.queryByRole("img", { name: "Mermaid" })).toBeNull()
    rerender(<MemoMarkdown text={fence()} />)
    const image = await screen.findByRole("img", { name: "Mermaid" })
    fireEvent.error(image)
    expect(screen.getByTestId("markdown-code").textContent).toContain(SOURCE)
  })

  it("does not turn ordinary text fences into diagrams", () => {
    render(<MemoMarkdown text={`\`\`\`text\n${SOURCE}\n\`\`\``} />)
    expect(screen.getByTestId("markdown-code").textContent).toContain(SOURCE)
    expect(mermaid.render).not.toHaveBeenCalled()
  })

  it("measures a diagram that finished loading before the listener attached", async () => {
    const client = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(function (this: HTMLElement) {
      return this.dataset.testid === "mermaid-viewport" ? 200 : 0
    })
    const naturalWidth = vi.spyOn(HTMLImageElement.prototype, "naturalWidth", "get").mockReturnValue(400)
    const naturalHeight = vi.spyOn(HTMLImageElement.prototype, "naturalHeight", "get").mockReturnValue(200)
    try {
      render(<MemoMarkdown text={fence()} />)
      const image = await screen.findByRole("img", { name: "Mermaid" })
      await waitFor(() => expect(image).toHaveStyle({ width: "200px", height: "100px" }))
      expect(screen.getByRole("button", { name: "Zoom in" })).toBeEnabled()
    } finally {
      client.mockRestore()
      naturalWidth.mockRestore()
      naturalHeight.mockRestore()
    }
  })

  it("fits the diagram to the reading frame and zooms from that size", async () => {
    const width = vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(function (this: HTMLElement) {
      return this.dataset.testid === "mermaid-viewport" ? 200 : 0
    })
    try {
      render(<MemoMarkdown text={fence()} />)
      const image = await screen.findByRole("img", { name: "Mermaid" })
      Object.defineProperty(image, "naturalWidth", { value: 400 })
      Object.defineProperty(image, "naturalHeight", { value: 200 })
      fireEvent.load(image)
      await waitFor(() => expect(image).toHaveStyle({ width: "200px", height: "100px" }))
      expect(screen.getByLabelText("Zoom level")).toHaveTextContent("50%")
      expect(screen.getByRole("button", { name: "Fit to window" })).toHaveAttribute("aria-pressed", "true")
      fireEvent.click(screen.getByRole("button", { name: "Zoom in" }))
      await waitFor(() => expect(image).toHaveStyle({ width: "300px", height: "150px" }))
      fireEvent.click(screen.getByRole("button", { name: "Actual size" }))
      await waitFor(() => expect(image).toHaveStyle({ width: "400px", height: "200px" }))
      fireEvent.click(screen.getByRole("button", { name: "Fit to window" }))
      await waitFor(() => expect(image).toHaveStyle({ width: "200px", height: "100px" }))
    } finally {
      width.mockRestore()
    }
  })

  it("renders a label whose identifier ends in url", async () => {
    const source = 'flowchart LR\n  A["itemurl(group)"] --> B["Shown"]'
    render(<MemoMarkdown text={fence(source)} />)
    expect(await screen.findByRole("img", { name: "Mermaid" })).toBeInTheDocument()
    expect(mermaid.render).toHaveBeenCalledWith(expect.stringMatching(/^mermaid-/), source, expect.any(HTMLElement))
  })

  it("keeps optional resource-bearing diagrams as source", async () => {
    render(<MemoMarkdown text={fence('flowchart LR\nA@{img:"https://example.invalid/private.png"}')} />)
    await act(async () => {})
    expect(screen.getByTestId("markdown-code").textContent).toContain("private.png")
    expect(mermaid.render).not.toHaveBeenCalled()
  })

  it("renders again after the resolved theme changes", async () => {
    let n = 0
    mermaid.render.mockImplementation(async () => ({ svg: SVG.replace("Start", `Start ${++n}`) }))
    render(<MemoMarkdown text={fence()} />)
    const image = await screen.findByRole("img", { name: "Mermaid" })
    const before = image.getAttribute("src")
    act(() => document.documentElement.classList.add("dark"))
    await waitFor(() => expect(screen.getByRole("img", { name: "Mermaid" })).not.toHaveAttribute("src", before))
    expect(mermaid.initialize).toHaveBeenLastCalledWith(expect.objectContaining({
      themeVariables: expect.objectContaining({ darkMode: true }),
    }))
  })

  it("serializes measurement and ignores an obsolete result after the source changes", async () => {
    let finish!: (value: { svg: string }) => void
    mermaid.render.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const { rerender } = render(<MemoMarkdown text={fence()} />)
    await waitFor(() => expect(mermaid.render).toHaveBeenCalledTimes(1))
    rerender(<MemoMarkdown text={fence("sequenceDiagram\n A->>B: Changed")} />)
    expect(mermaid.render).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole("img", { name: "Mermaid" })).toBeNull()
    await act(async () => finish({ svg: SVG.replace("Start", "Obsolete") }))
    const image = await screen.findByRole("img", { name: "Mermaid" })
    expect(decodeURIComponent(image.getAttribute("src")!)).not.toContain("Obsolete")
    expect(mermaid.render).toHaveBeenCalledTimes(2)
    expect(new Set(mermaid.render.mock.calls.map(([id]) => id)).size).toBe(2)
    expect(document.querySelector(".md-mermaid-measure")).toBeNull()
  })

  it("does not start a queued render after its view was unmounted", async () => {
    let finish!: (value: { svg: string }) => void
    mermaid.render.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    render(<MemoMarkdown text={fence()} />)
    await waitFor(() => expect(mermaid.render).toHaveBeenCalledTimes(1))
    const second = render(<MemoMarkdown text={fence("sequenceDiagram\n A->>B: Other")} />)
    await act(async () => {})
    second.unmount()
    await act(async () => finish({ svg: SVG }))
    await screen.findByRole("img", { name: "Mermaid" })
    expect(mermaid.render).toHaveBeenCalledTimes(1)
  })
})
