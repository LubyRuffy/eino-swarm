import { afterEach, describe, expect, it } from "vitest"

import {
  attachExternalLinkHandler,
  classifyHref,
} from "./external-links"

const ORIGIN = "http://127.0.0.1:8787"

describe("classifyHref", () => {
  it.each([
    ["#fn-1", "stay"],
    ["/api/threads/th_1/download/a.txt", "stay"],
    ["https://example.invalid/docs", "leave"],
    ["http://example.invalid/docs", "leave"],
    ["mailto:user@example.invalid", "leave"],
    ["javascript:alert(1)", "block"],
    ["data:text/html,hi", "block"],
    ["file:///etc/passwd", "block"],
    ["", "stay"],
  ] as const)("%s is %s", (href, want) => {
    expect(classifyHref(href, ORIGIN)).toBe(want)
  })

  it("treats a missing href as something that stays in the app", () => {
    expect(classifyHref(undefined, ORIGIN)).toBe("stay")
  })
})

describe("attachExternalLinkHandler", () => {
  const cleanups: Array<() => void> = []

  afterEach(() => {
    while (cleanups.length) cleanups.pop()?.()
    document.body.replaceChildren()
  })

  function link(href: string, attrs?: Record<string, string>) {
    const a = document.createElement("a")
    a.textContent = "go"
    a.setAttribute("href", href)
    for (const [k, v] of Object.entries(attrs ?? {})) a.setAttribute(k, v)
    document.body.appendChild(a)
    return a
  }

  function attach(opts: Parameters<typeof attachExternalLinkHandler>[0] = {}) {
    const opened: string[] = []
    const native: string[] = []
    const nativeFn = opts.openNative
    const detach = attachExternalLinkHandler({
      ...opts,
      origin: opts.origin ?? ORIGIN,
      openWindow: opts.openWindow ?? ((url) => opened.push(url)),
      openNative: nativeFn
        ? (url) => {
            native.push(url)
            nativeFn(url)
          }
        : undefined,
    })
    cleanups.push(detach)
    return { opened, native }
  }

  function click(a: HTMLAnchorElement) {
    const ev = new MouseEvent("click", { bubbles: true, cancelable: true, button: 0 })
    a.dispatchEvent(ev)
    return ev
  }

  it("opens an http link in a new window instead of replacing this one", () => {
    const a = link("https://example.invalid/docs")
    const { opened, native } = attach()
    expect(click(a).defaultPrevented).toBe(true)
    expect(opened).toEqual(["https://example.invalid/docs"])
    expect(native).toEqual([])
  })

  it("asks the desktop shell to open the system browser", () => {
    const a = link("https://example.invalid/docs")
    const { opened, native } = attach({
      openNative: () => undefined,
    })
    expect(click(a).defaultPrevented).toBe(true)
    expect(native).toEqual(["https://example.invalid/docs"])
    expect(opened).toEqual([])
  })

  it("leaves a fragment link to the page", () => {
    const a = link("#fn-1")
    const { opened, native } = attach({ openNative: () => undefined })
    expect(click(a).defaultPrevented).toBe(false)
    expect(opened).toEqual([])
    expect(native).toEqual([])
  })

  it("leaves a same-origin download to the browser", () => {
    const a = link("#file", { download: "" })
    const { opened, native } = attach({ openNative: () => undefined })
    expect(click(a).defaultPrevented).toBe(false)
    expect(opened).toEqual([])
    expect(native).toEqual([])
  })

  it("does not turn a javascript: href into a navigation", () => {
    const a = link("javascript:void(0)")
    const { opened, native } = attach({ openNative: () => undefined })
    expect(click(a).defaultPrevented).toBe(true)
    expect(opened).toEqual([])
    expect(native).toEqual([])
  })
})
