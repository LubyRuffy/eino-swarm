import { afterEach, describe, expect, it, vi } from "vitest"

import { copyText } from "./copy-text"

describe("copyText", () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    Reflect.deleteProperty(document, "execCommand")
  })

  it("copies through execCommand while the click is still a user gesture", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal("navigator", { ...navigator, clipboard: { writeText } })
    const exec = stubExecCommand((cmd) => {
      expect(cmd).toBe("copy")
      const el = document.activeElement
      expect(el).toBeInstanceOf(HTMLTextAreaElement)
      expect((el as HTMLTextAreaElement).value).toBe("alpha")
      return true
    })
    await expect(copyText("alpha")).resolves.toBe(true)
    expect(exec).toHaveBeenCalledWith("copy")
    expect(writeText).not.toHaveBeenCalled()
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

  it("uses execCommand when the clipboard API is missing", async () => {
    vi.stubGlobal("navigator", { ...navigator, clipboard: undefined })
    const exec = stubExecCommand(() => true)
    await expect(copyText("alpha")).resolves.toBe(true)
    expect(exec).toHaveBeenCalledWith("copy")
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
