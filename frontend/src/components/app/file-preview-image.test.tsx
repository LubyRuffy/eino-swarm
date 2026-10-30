import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FilePreviewImage } from "./file-preview-image"

let resize: () => void
let viewportWidth = 600
let viewportHeight = 400
const disconnect = vi.fn()

beforeEach(() => {
  viewportWidth = 600
  viewportHeight = 400
  disconnect.mockClear()
  vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockImplementation(() => viewportWidth)
  vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockImplementation(() => viewportHeight)
  vi.stubGlobal("ResizeObserver", class {
    constructor(callback: () => void) { resize = callback }
    observe() {}
    disconnect = disconnect
  })
})
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals() })

function showImage(width = 1200, height = 800) {
  const rendered = render(<FilePreviewImage src="blob:picture" name="picture.png" />)
  const image = screen.getByRole("img", { name: "picture.png" }) as HTMLImageElement
  Object.defineProperties(image, { naturalWidth: { value: width }, naturalHeight: { value: height } })
  fireEvent.load(image)
  return { ...rendered, image }
}

const button = (name: string) => screen.getByRole("button", { name })

describe("Image preview controls", () => {
  it("waits for decoded dimensions before enabling image controls", () => {
    render(<FilePreviewImage src="blob:picture" name="picture.png" />)
    expect(screen.getByText("Loading image…")).toBeInTheDocument()
    for (const name of ["Zoom in", "Zoom out", "Actual size", "Fit to window"]) expect(button(name)).toBeDisabled()
    expect(screen.getByLabelText("Zoom level")).toHaveTextContent("—")
  })

  it("fits large images as the viewport changes and keeps manual pixel size", () => {
    const { image } = showImage()
    expect(image).toHaveStyle({ width: "600px", height: "400px" })
    expect(screen.getByLabelText("Zoom level")).toHaveTextContent("50%")
    viewportWidth = 300
    viewportHeight = 200
    act(() => resize())
    expect(image).toHaveStyle({ width: "300px", height: "200px" })
    fireEvent.click(button("Actual size"))
    expect(image).toHaveStyle({ width: "1200px", height: "800px" })
    expect(button("Fit to window")).toHaveAttribute("aria-pressed", "false")
    viewportWidth = 600
    viewportHeight = 400
    act(() => resize())
    expect(image).toHaveStyle({ width: "1200px", height: "800px" })
    fireEvent.click(button("Fit to window"))
    expect(image).toHaveStyle({ width: "600px", height: "400px" })
    const viewport = screen.getByTestId("image-preview-viewport")
    expect([viewport.scrollLeft, viewport.scrollTop]).toEqual([0, 0])
  })

  it("does not enlarge small images when fitting", () => {
    const { image } = showImage(200, 100)
    expect(image).toHaveStyle({ width: "200px", height: "100px" })
    expect(screen.getByLabelText("Zoom level")).toHaveTextContent("100%")
  })

  it("bounds manual zoom and centres the enlarged image inside its scroll area", () => {
    const { image } = showImage()
    fireEvent.click(button("Actual size"))
    const viewport = screen.getByTestId("image-preview-viewport")
    expect([viewport.scrollLeft, viewport.scrollTop]).toEqual([300, 200])
    for (let i = 0; i < 20; i++) fireEvent.click(button("Zoom in"))
    expect(button("Zoom in")).toBeDisabled()
    expect(image).toHaveStyle({ width: "4800px", height: "3200px" })
    expect(screen.getByLabelText("Zoom level")).toHaveTextContent("400%")
    for (let i = 0; i < 20; i++) fireEvent.click(button("Zoom out"))
    expect(button("Zoom out")).toBeDisabled()
    expect(image).toHaveStyle({ width: "120px", height: "80px" })
    expect(screen.getByLabelText("Zoom level")).toHaveTextContent("10%")
  })

  it("fits very large images below the manual zoom minimum without clipping", () => {
    const { image } = showImage(12000, 8000)
    expect(image).toHaveStyle({ width: "600px", height: "400px" })
    expect(screen.getByLabelText("Zoom level")).toHaveTextContent("5%")
    expect(button("Zoom out")).toBeDisabled()
    fireEvent.click(button("Zoom in"))
    expect(screen.getByLabelText("Zoom level")).toHaveTextContent("30%")
  })

  it("keeps a decode failure explicit and disables image controls", () => {
    const { image } = showImage()
    fireEvent.error(image)
    expect(screen.getByRole("alert")).toHaveTextContent("This image could not be displayed")
    for (const name of ["Zoom in", "Zoom out", "Actual size", "Fit to window"]) expect(button(name)).toBeDisabled()
    expect(screen.queryByRole("img")).not.toBeInTheDocument()
  })

  it("refuses a load event without decoded image dimensions", () => {
    render(<FilePreviewImage src="blob:broken" name="broken.png" />)
    fireEvent.load(screen.getByRole("img"))
    expect(screen.getByRole("alert")).toBeInTheDocument()
    expect(button("Zoom in")).toBeDisabled()
  })

  it("resets fit for a fresh image and releases the old resize observer", () => {
    const { rerender } = showImage()
    fireEvent.click(button("Actual size"))
    rerender(<FilePreviewImage key="new-image" src="blob:new" name="new.png" />)
    expect(disconnect).toHaveBeenCalledOnce()
    expect(button("Actual size")).toBeDisabled()
    const image = screen.getByRole("img")
    Object.defineProperties(image, { naturalWidth: { value: 1200 }, naturalHeight: { value: 800 } })
    fireEvent.load(image)
    expect(image).toHaveStyle({ width: "600px", height: "400px" })
    expect(button("Fit to window")).toHaveAttribute("aria-pressed", "true")
  })
})
