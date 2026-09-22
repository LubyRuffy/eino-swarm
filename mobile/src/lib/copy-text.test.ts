import { afterEach, describe, expect, it, vi } from "vitest"

import { copyText } from "./copy-text"

describe("copyText", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    Reflect.deleteProperty(document, "execCommand")
  })

  it("plants the payload on the copy event while the click is still a user gesture", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    const setData = vi.fn()
    const exec = stubExecCommand((cmd) => {
      expect(cmd).toBe("copy")
      const el = document.activeElement
      expect(el).toBeInstanceOf(HTMLTextAreaElement)
      expect((el as HTMLTextAreaElement).value).toBe("alpha")
      fireCopyEvent(setData)
      return true
    })
    await expect(copyText("alpha")).resolves.toBe(true)
    expect(exec).toHaveBeenCalledWith("copy")
    expect(setData).toHaveBeenCalledWith("text/plain", "alpha")
    expect(writeText).not.toHaveBeenCalled()
  })

  it("does not treat a silent execCommand success as copied", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    stubExecCommand(() => true)
    await expect(copyText("alpha")).resolves.toBe(true)
    expect(writeText).toHaveBeenCalledWith("alpha")
  })

  it("uses the clipboard API when execCommand cannot copy", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    stubExecCommand(() => false)
    await expect(copyText("alpha")).resolves.toBe(true)
    expect(writeText).toHaveBeenCalledWith("alpha")
  })

  it("is false when both paths fail", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"))
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    stubExecCommand(() => false)
    await expect(copyText("alpha")).resolves.toBe(false)
  })

  it("is false when execCommand returns true without a copy event and write is denied", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"))
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    stubExecCommand(() => true)
    await expect(copyText("alpha")).resolves.toBe(false)
    expect(writeText).toHaveBeenCalledWith("alpha")
  })

  it("uses execCommand when the clipboard API is missing", async () => {
    vi.stubGlobal("navigator", { ...navigator, clipboard: undefined })
    const setData = vi.fn()
    const exec = stubExecCommand((cmd) => {
      fireCopyEvent(setData)
      return cmd === "copy"
    })
    await expect(copyText("alpha")).resolves.toBe(true)
    expect(exec).toHaveBeenCalledWith("copy")
    expect(setData).toHaveBeenCalledWith("text/plain", "alpha")
  })
})

function stubExecCommand(impl: (cmd: string) => boolean) {
  const exec = vi.fn(impl)
  Object.defineProperty(document, "execCommand", {
    configurable: true,
    writable: true,
    value: exec,
  })
  return exec
}

function fireCopyEvent(setData: (type: string, value: string) => void) {
  const ev = new Event("copy", { bubbles: true, cancelable: true })
  Object.defineProperty(ev, "clipboardData", {
    value: { setData },
  })
  document.dispatchEvent(ev)
}
