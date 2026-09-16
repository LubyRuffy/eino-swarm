import { afterEach, describe, expect, it, vi } from "vitest"

import {
  installTitlebarZoom,
  isTitlebarDragEvent,
  nativeTitlebarInvoke,
  TITLEBAR_DOUBLE_CLICK,
} from "./titlebar"

function dragRegion(html: string): HTMLElement {
  document.body.innerHTML = html
  const region = document.querySelector("[data-drag-region]")
  if (!(region instanceof HTMLElement)) {
    throw new Error("fixture is missing data-drag-region")
  }
  return region
}

function eventOn(target: EventTarget | null): MouseEvent {
  const event = new MouseEvent("dblclick", { bubbles: true, cancelable: true })
  Object.defineProperty(event, "target", { value: target })
  return event
}

function dblclick(target: EventTarget): boolean {
  return target.dispatchEvent(
    new MouseEvent("dblclick", { bubbles: true, cancelable: true }),
  )
}

describe("isTitlebarDragEvent", () => {
  afterEach(() => {
    document.body.innerHTML = ""
  })

  it("treats the title bar chrome as a zoom target", () => {
    const region = dragRegion(
      `<header data-drag-region><p data-testid="title">Hello</p></header>`,
    )
    const title = region.querySelector("[data-testid='title']")
    expect(isTitlebarDragEvent(eventOn(title))).toBe(true)
    expect(isTitlebarDragEvent(eventOn(region))).toBe(true)
  })

  it("does not zoom a control sitting in the chrome", () => {
    dragRegion(
      `<header data-drag-region>
         <button type="button">theme</button>
         <div data-no-drag><span>island</span></div>
       </header>`,
    )
    expect(isTitlebarDragEvent(eventOn(document.querySelector("button")))).toBe(false)
    expect(isTitlebarDragEvent(eventOn(document.querySelector("[data-no-drag] span")))).toBe(
      false,
    )
  })

  it("ignores a double-click that is not on the window chrome", () => {
    document.body.innerHTML = `<p data-testid="body">not chrome</p>`
    expect(isTitlebarDragEvent(eventOn(document.querySelector("[data-testid='body']")))).toBe(
      false,
    )
  })
})

describe("installTitlebarZoom", () => {
  let stop: () => void

  afterEach(() => {
    stop?.()
    document.body.innerHTML = ""
  })

  // Dragging is native; this is the missing half: the second click has to
  // tell the window to zoom, or the title bar is a dead zone.
  it("asks the native window to zoom on a title-bar double-click", () => {
    const send = vi.fn()
    stop = installTitlebarZoom(send)
    dragRegion(`<header data-drag-region><p data-testid="title">Hello</p></header>`)
    const defaulted = dblclick(document.querySelector("[data-testid='title']")!)
    expect(send).toHaveBeenCalledWith(TITLEBAR_DOUBLE_CLICK)
    expect(defaulted).toBe(false)
  })

  it("leaves buttons and the rest of the page alone", () => {
    const send = vi.fn()
    stop = installTitlebarZoom(send)
    dragRegion(
      `<header data-drag-region><button type="button">theme</button></header>
       <p data-testid="body">not chrome</p>`,
    )
    dblclick(document.querySelector("button")!)
    dblclick(document.querySelector("[data-testid='body']")!)
    expect(send).not.toHaveBeenCalled()
  })

  it("does nothing in a browser, where there is no native window to zoom", () => {
    stop = installTitlebarZoom()
    dragRegion(`<header data-drag-region><p data-testid="title">Hello</p></header>`)
    expect(() => dblclick(document.querySelector("[data-testid='title']")!)).not.toThrow()
  })
})

describe("nativeTitlebarInvoke", () => {
  afterEach(() => {
    delete (window as Window & { _wails?: unknown })._wails
    delete (window as Window & { webkit?: unknown }).webkit
    delete (window as Window & { chrome?: unknown }).chrome
  })

  it("uses the Wails invoke stub when it is there", () => {
    const invoke = vi.fn()
    ;(window as Window & { _wails?: { invoke: typeof invoke } })._wails = { invoke }
    nativeTitlebarInvoke()?.(TITLEBAR_DOUBLE_CLICK)
    expect(invoke).toHaveBeenCalledWith(TITLEBAR_DOUBLE_CLICK)
  })

  it("falls back to the webkit bridge the desktop window always has", () => {
    const postMessage = vi.fn()
    ;(
      window as Window & {
        webkit?: { messageHandlers: { external: { postMessage: typeof postMessage } } }
      }
    ).webkit = { messageHandlers: { external: { postMessage } } }
    nativeTitlebarInvoke()?.(TITLEBAR_DOUBLE_CLICK)
    expect(postMessage).toHaveBeenCalledWith(TITLEBAR_DOUBLE_CLICK)
  })

  it("is missing in a browser tab", () => {
    expect(nativeTitlebarInvoke()).toBeUndefined()
  })
})
