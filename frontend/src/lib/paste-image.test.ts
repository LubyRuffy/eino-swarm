import { describe, expect, it, vi } from "vitest"

import {
  MAX_PASTE_IMAGE_BYTES,
  MAX_PASTE_IMAGES,
  addPasteImages,
  dropPasteImage,
  fileToBase64,
  filesFromClipboard,
  isPasteImage,
  pasteImageFromFile,
  plainTextFromClipboard,
  recoverDroppedPaste,
  revokePasteImages,
  toSendImages,
} from "./paste-image"

function file(name: string, type: string, bytes: number | Uint8Array = 4): File {
  const data = typeof bytes === "number" ? new Uint8Array(bytes) : bytes
  return new File([data as BlobPart], name, { type })
}

describe("isPasteImage", () => {
  it("accepts image MIME types and named images with an empty type", () => {
    expect(isPasteImage(file("a.png", "image/png"))).toBe(true)
    expect(isPasteImage(file("a.jpg", "image/jpeg"))).toBe(true)
    expect(isPasteImage(file("shot.PNG", ""))).toBe(true)
    expect(isPasteImage(file("notes.md", "text/plain"))).toBe(false)
    expect(isPasteImage(file("x.bin", "application/octet-stream"))).toBe(false)
  })
})

describe("plainTextFromClipboard", () => {
  it("reads text/plain and treats a missing clipboard as empty", () => {
    const data = {
      getData: (type: string) => (type === "text/plain" ? "alpha" : ""),
    } as unknown as DataTransfer
    expect(plainTextFromClipboard(data)).toBe("alpha")
    expect(plainTextFromClipboard(null)).toBe("")
    expect(plainTextFromClipboard(undefined)).toBe("")
  })

  it("is empty when the clipboard throws", () => {
    const data = {
      getData: () => {
        throw new Error("denied")
      },
    } as unknown as DataTransfer
    expect(plainTextFromClipboard(data)).toBe("")
  })
})

describe("recoverDroppedPaste", () => {
  // The paste event fired and the textarea did not change. That is WKWebView
  // dropping the insert on an empty box, not an empty clipboard.
  it("inserts when the browser left the field unchanged", () => {
    expect(recoverDroppedPaste("", 0, 0, "", "alpha")).toEqual({
      text: "alpha",
      caret: 5,
    })
  })

  it("inserts into a selection the browser did not replace", () => {
    expect(recoverDroppedPaste("abcd", 1, 3, "abcd", "X")).toEqual({
      text: "aXd",
      caret: 2,
    })
  })

  it("does nothing when the browser already inserted", () => {
    expect(recoverDroppedPaste("", 0, 0, "alpha", "alpha")).toBeNull()
  })

  it("does nothing for an empty payload", () => {
    expect(recoverDroppedPaste("", 0, 0, "", "")).toBeNull()
  })
})

describe("filesFromClipboard", () => {
  it("pulls image files and ignores everything else", () => {
    const png = file("clip.png", "image/png")
    const txt = file("notes.txt", "text/plain")
    const data = {
      items: [
        { kind: "string", type: "text/plain", getAsFile: () => null },
        { kind: "file", type: "image/png", getAsFile: () => png },
        { kind: "file", type: "text/plain", getAsFile: () => txt },
      ],
      files: [png, txt],
    } as unknown as DataTransfer
    expect(filesFromClipboard(data)).toEqual([png])
    expect(filesFromClipboard(null)).toEqual([])
  })
})

function stubObjectURLs() {
  const create = vi.fn(() => "blob:preview")
  const revoke = vi.fn()
  Object.defineProperty(URL, "createObjectURL", {
    configurable: true,
    writable: true,
    value: create,
  })
  Object.defineProperty(URL, "revokeObjectURL", {
    configurable: true,
    writable: true,
    value: revoke,
  })
  return { create, revoke }
}

describe("addPasteImages", () => {
  it("caps how many can sit in the composer and skips oversize dumps", () => {
    const { revoke } = stubObjectURLs()
    const first = addPasteImages([], [file("a.png", "image/png")])
    expect(first.next).toHaveLength(1)
    expect(first.skipped).toBe(0)

    const huge = file("big.png", "image/png", MAX_PASTE_IMAGE_BYTES + 1)
    const overflow: File[] = []
    for (let i = 0; i < MAX_PASTE_IMAGES; i++) {
      overflow.push(file(`n${i}.png`, "image/png"))
    }
    overflow.push(huge)
    const next = addPasteImages(first.next, overflow)
    expect(next.next).toHaveLength(MAX_PASTE_IMAGES)
    expect(next.skipped).toBeGreaterThan(0)

    const gone = dropPasteImage(next.next, next.next[0].id)
    expect(gone).toHaveLength(MAX_PASTE_IMAGES - 1)
    expect(revoke).toHaveBeenCalled()
    revokePasteImages(gone)
  })
})

describe("toSendImages", () => {
  it("base64-encodes the pixels the wire body expects", async () => {
    stubObjectURLs()
    const bytes = new Uint8Array([1, 2, 3, 4])
    const img = pasteImageFromFile(file("clip.png", "image/png", bytes))
    const payload = await toSendImages([img])
    expect(payload).toEqual([
      { name: "clip.png", mime: "image/png", data: await fileToBase64(img.file) },
    ])
    expect(payload[0].data).toBe(btoa(String.fromCharCode(1, 2, 3, 4)))
  })
})
