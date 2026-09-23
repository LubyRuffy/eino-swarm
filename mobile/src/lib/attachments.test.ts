import { describe, expect, it } from "vitest"

import { AttachmentTooBig, looksLikeText, readComposerFiles, turnsFromMessages } from "./attachments"
import { maxAttachmentBytes } from "./attachments"
import type { DirectMessage } from "./direct-threads"

describe("attachments", () => {
  it("treats an image as vision and a text file as text", async () => {
    const image = new File([Uint8Array.from([1, 2, 3])], "shot.png", { type: "image/png" })
    const notes = new File(["alpha"], "notes.txt", { type: "text/plain" })
    const read = await readComposerFiles([image, notes])
    expect(read.chips.map((chip) => chip.kind)).toEqual(["image", "file"])
    expect(read.parts[0].kind).toBe("image")
    expect(read.parts[1]).toMatchObject({ kind: "text", text: "notes.txt\nalpha" })
  })

  it("keeps a binary file as a file part", async () => {
    const pdf = new File([Uint8Array.from([0x25, 0x50, 0x44, 0x46])], "a.pdf", {
      type: "application/pdf",
    })
    const read = await readComposerFiles([pdf])
    expect(read.parts[0].kind).toBe("file")
    if (read.parts[0].kind === "file") expect(read.parts[0].dataUrl).toContain("base64,")
  })

  it("refuses a file over the send cap", async () => {
    const big = new File([new Uint8Array(8)], "big.bin", { type: "application/octet-stream" })
    Object.defineProperty(big, "size", { value: maxAttachmentBytes + 1 })
    await expect(readComposerFiles([big])).rejects.toBeInstanceOf(AttachmentTooBig)
  })

  it("sniffs text when the browser did not set a type", () => {
    expect(looksLikeText(new TextEncoder().encode("abc"), "")).toBe(true)
    expect(looksLikeText(Uint8Array.from([0, 1]), "")).toBe(false)
    expect(looksLikeText(Uint8Array.from([1]), "application/pdf")).toBe(false)
  })

  it("rebuilds a turn from the message, skipping an empty error", () => {
    const messages: DirectMessage[] = [
      {
        id: "u",
        role: "user",
        text: "see",
        attachments: [{ name: "shot.png", kind: "image", dataUrl: "data:image/png;base64,aa" }],
      },
      { id: "e", role: "assistant", text: "", error: "nope" },
    ]
    const turns = turnsFromMessages(messages)
    expect(turns).toHaveLength(1)
    expect(turns[0].parts.map((part) => part.kind)).toEqual(["text", "image"])
  })
})
