import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { t } from "@/lib/i18n"
import { ImageFilePreview } from "./image-file-preview"

let resize: () => void
let viewportWidth = 320
let viewportHeight = 480
const disconnect = vi.fn()

beforeEach(() => {
  viewportWidth = 320
  viewportHeight = 480
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

const zoomIn = () => screen.getByRole("button", { name: t("files.zoomIn") })
const zoomOut = () => screen.getByRole("button", { name: t("files.zoomOut") })
const actualSize = () => screen.getByRole("button", { name: t("files.actualSize") })
const fit = () => screen.getByRole("button", { name: t("files.fit") })
const level = () => screen.getByLabelText(t("files.zoomLevel"))

function showImage(width = 1280, height = 960) {
  const rendered = render(<ImageFilePreview src="blob:picture" name="picture.png" />)
  const image = screen.getByRole("img", { name: "picture.png" }) as HTMLImageElement
  Object.defineProperties(image, { naturalWidth: { value: width }, naturalHeight: { value: height } })
  fireEvent.load(image)
  return { ...rendered, image }
}

describe("phone image preview tools", () => {
  it("waits for decoded dimensions before enabling zoom", () => {
    render(<ImageFilePreview src="blob:picture" name="picture.png" />)
    expect(screen.getByText(t("files.imageLoading"))).toHaveAttribute("role", "status")
    for (const button of [zoomIn(), zoomOut(), actualSize(), fit()]) expect(button).toBeDisabled()
    expect(level()).toHaveTextContent("—")
    expect(screen.getByRole("region", { name: t("files.imageViewport") })).toHaveAttribute("aria-busy", "true")
  })

  it("follows viewport size in fit mode while preserving manual pixel size", () => {
    const { image } = showImage()
    expect(image).toHaveStyle({ width: "320px", height: "240px" })
    expect(level()).toHaveTextContent("25%")
    viewportWidth = 640
    viewportHeight = 480
    act(() => resize())
    expect(image).toHaveStyle({ width: "640px", height: "480px" })
    fireEvent.click(actualSize())
    expect(image).toHaveStyle({ width: "1280px", height: "960px" })
    expect(fit()).toHaveAttribute("aria-pressed", "false")
    viewportWidth = 320
    viewportHeight = 480
    act(() => resize())
    expect(image).toHaveStyle({ width: "1280px", height: "960px" })
    fireEvent.click(fit())
    expect(image).toHaveStyle({ width: "320px", height: "240px" })
    expect(fit()).toHaveAttribute("aria-pressed", "true")
  })

  it("keeps a small image at its original dimensions when fitting", () => {
    const { image } = showImage(120, 80)
    expect(image).toHaveStyle({ width: "120px", height: "80px" })
    expect(level()).toHaveTextContent("100%")
  })

  it("centres an enlarged image in a scrollport and bounds manual zoom", () => {
    const { image } = showImage()
    fireEvent.click(actualSize())
    const viewport = screen.getByRole("region", { name: t("files.imageViewport") })
    expect([viewport.scrollLeft, viewport.scrollTop]).toEqual([480, 240])
    expect(viewport).toHaveClass("overflow-auto")
    expect(viewport).toHaveAttribute("tabindex", "0")
    expect(viewport).not.toContainElement(screen.getByRole("group", { name: t("files.imageControls") }))
    for (let i = 0; i < 20; i++) fireEvent.click(zoomIn())
    expect(zoomIn()).toBeDisabled()
    expect(level()).toHaveTextContent("400%")
    expect(image).toHaveStyle({ width: "5120px", height: "3840px" })
    for (let i = 0; i < 20; i++) fireEvent.click(zoomOut())
    expect(zoomOut()).toBeDisabled()
    expect(level()).toHaveTextContent("10%")
    expect(image).toHaveStyle({ width: "128px", height: "96px" })
    expect(image).toHaveAttribute("src", "blob:picture")
  })

  it("fits a large image below the manual minimum without cropping", () => {
    const { image } = showImage(12800, 9600)
    expect(image).toHaveStyle({ width: "320px", height: "240px" })
    expect(level()).toHaveTextContent("2.5%")
    expect(zoomOut()).toBeDisabled()
    fireEvent.click(zoomIn())
    expect(level()).toHaveTextContent("28%")
  })

  it("shows decode failure and disables every image action", () => {
    const { image } = showImage()
    fireEvent.error(image)
    expect(screen.getByRole("alert")).toHaveTextContent(t("files.invalidImage"))
    for (const button of [zoomIn(), zoomOut(), actualSize(), fit()]) expect(button).toBeDisabled()
    expect(screen.queryByRole("img")).not.toBeInTheDocument()
    expect(screen.getByRole("region")).toHaveAttribute("aria-busy", "false")
  })

  it("rejects a load event with no decodable dimensions", () => {
    render(<ImageFilePreview src="blob:broken" name="broken.png" />)
    fireEvent.load(screen.getByRole("img"))
    expect(screen.getByRole("alert")).toHaveTextContent(t("files.invalidImage"))
    expect(zoomIn()).toBeDisabled()
  })

  it("resets to fit on a new file and disconnects the old viewport observer", () => {
    const { rerender } = showImage()
    fireEvent.click(actualSize())
    rerender(<ImageFilePreview key="new-file" src="blob:new" name="new.png" />)
    expect(disconnect).toHaveBeenCalledOnce()
    expect(actualSize()).toBeDisabled()
    const image = screen.getByRole("img")
    Object.defineProperties(image, { naturalWidth: { value: 1280 }, naturalHeight: { value: 960 } })
    fireEvent.load(image)
    expect(image).toHaveStyle({ width: "320px", height: "240px" })
    expect(fit()).toHaveAttribute("aria-pressed", "true")
  })
})
