import { beforeEach, describe, expect, it, vi } from "vitest"

const native = vi.hoisted(() => ({ enabled: true, saveFile: vi.fn() }))
vi.mock("@capacitor/core", () => ({
  Capacitor: { isNativePlatform: () => native.enabled },
  registerPlugin: () => ({ saveFile: native.saveFile }),
}))

import { savePhoneFile } from "./file-save"
import { maxPhoneFileBytes } from "./remote-files"

describe("native file export", () => {
  beforeEach(() => {
    native.enabled = true
    native.saveFile.mockReset().mockResolvedValue({ saved: true })
  })

  it("preserves a valid filename's spaces and a byte view's exact boundaries", async () => {
    const backing = new Uint8Array([42, 0, 255, 128, 43])
    await expect(savePhoneFile(" 报告.bin ", "application/octet-stream", backing.subarray(1, 4))).resolves.toBe(true)
    expect(native.saveFile).toHaveBeenCalledWith({
      name: " 报告.bin ", mime: "application/octet-stream", data: "AP+A", size: 3,
    })
  })

  it.each(["", ".", "..", "../report.md", "folder/report.md", "folder\\report.md", "report\u0000.md", "report\n.md"])(
    "refuses a filename with path or control semantics instead of renaming it", async (name) => {
      await expect(savePhoneFile(name, "text/plain", new Uint8Array())).rejects.toThrow("invalid_file_name")
      expect(native.saveFile).not.toHaveBeenCalled()
    },
  )

  it("refuses a file beyond the established phone read limit before crossing the bridge", async () => {
    await expect(savePhoneFile("large.bin", "application/octet-stream", new Uint8Array(maxPhoneFileBytes + 1))).rejects.toThrow("file_too_large")
    expect(native.saveFile).not.toHaveBeenCalled()
  })

  it("does not treat a malformed native result as a saved file", async () => {
    native.saveFile.mockResolvedValueOnce({})
    await expect(savePhoneFile("result.bin", "application/octet-stream", new Uint8Array())).rejects.toThrow("invalid_file_save_result")
  })

  it("exports an unknown MIME as bytes and preserves a real cancellation", async () => {
    native.saveFile.mockResolvedValueOnce({ saved: false })
    await expect(savePhoneFile("result.bin", "text/plain\r\nother", new Uint8Array())).resolves.toBe(false)
    expect(native.saveFile).toHaveBeenCalledWith({ name: "result.bin", mime: "application/octet-stream", data: "", size: 0 })
  })
})
