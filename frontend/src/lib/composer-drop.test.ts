import { describe, expect, it } from "vitest"

import {
  filesFromDataTransfer,
  isFileDrag,
  splitDroppedFiles,
} from "./composer-drop"

function file(name: string, type: string, bytes = 4): File {
  return new File([new Uint8Array(bytes)], name, { type })
}

function transfer(partial: {
  types?: string[]
  files?: File[]
  items?: Array<{
    kind: string
    type: string
    getAsFile: () => File | null
    webkitGetAsEntry?: () => { isDirectory?: boolean } | null
  }>
}): DataTransfer {
  return partial as unknown as DataTransfer
}

describe("isFileDrag", () => {
  it("arms for a file drag and ignores dragged text", () => {
    expect(isFileDrag(transfer({ types: ["Files"] }))).toBe(true)
    expect(
      isFileDrag(
        transfer({
          types: {
            length: 1,
            contains: (t: string) => t === "Files",
          } as unknown as string[],
        }),
      ),
    ).toBe(true)
    expect(isFileDrag(transfer({ types: ["text/plain"] }))).toBe(false)
    expect(isFileDrag(null)).toBe(false)
  })
})

describe("filesFromDataTransfer", () => {
  it("collects dropped files once, skipping folders", () => {
    const png = file("shot.png", "image/png")
    const notes = file("notes.txt", "text/plain")
    const folder = file("docs", "")
    const data = transfer({
      items: [
        { kind: "string", type: "text/plain", getAsFile: () => null },
        { kind: "file", type: png.type, getAsFile: () => png },
        { kind: "file", type: notes.type, getAsFile: () => notes },
        {
          kind: "file",
          type: "",
          getAsFile: () => folder,
          webkitGetAsEntry: () => ({ isDirectory: true }),
        },
      ],
      files: [png, notes, folder],
    })
    expect(filesFromDataTransfer(data)).toEqual([png, notes])
    expect(filesFromDataTransfer(null)).toEqual([])
  })

  it("falls back to the files list when items are empty", () => {
    const notes = file("notes.txt", "text/plain")
    expect(filesFromDataTransfer(transfer({ files: [notes], items: [] }))).toEqual([
      notes,
    ])
  })
})

describe("splitDroppedFiles", () => {
  it("sends images as vision and everything else as workspace attachments", () => {
    const png = file("shot.png", "image/png")
    const jpeg = file("photo.JPG", "")
    const notes = file("notes.txt", "text/plain")
    expect(splitDroppedFiles([png, jpeg, notes])).toEqual({
      images: [png, jpeg],
      attachments: [notes],
    })
  })
})
