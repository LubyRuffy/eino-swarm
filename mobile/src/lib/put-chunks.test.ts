import { describe, expect, it } from "vitest"

import { chunkBytes, fullChunkFits, isVisionFile, putFrames, putName } from "./put-chunks"

describe("put chunks", () => {
  it("keeps a full chunk under a pairlink frame", () => {
    expect(fullChunkFits()).toBe(true)
  })

  it("splits on the chunk size and round-trips the bytes", () => {
    const bytes = new Uint8Array([1, 2, 3, 4, 5])
    expect(chunkBytes(bytes, 2).map((p) => [...p])).toEqual([[1, 2], [3, 4], [5]])
    const frames = putFrames("up", "notes.txt", "text/plain", bytes)
    expect(frames).toHaveLength(1)
    expect(frames[0].part).toBe(1)
    expect(frames[0].parts).toBe(1)
    expect(frames[0].put_id).toBe("up")
    const joined = frames
      .map((f) => Uint8Array.from(atob(f.data ?? ""), (c) => c.charCodeAt(0)))
      .reduce((acc, part) => {
        const next = new Uint8Array(acc.length + part.length)
        next.set(acc)
        next.set(part, acc.length)
        return next
      }, new Uint8Array())
    expect([...joined]).toEqual([1, 2, 3, 4, 5])
  })

  it("uses the last path segment and does not treat a text name as an image", () => {
    expect(putName("dir/notes.txt", "text/plain")).toBe("notes.txt")
    expect(putName("..", "image/png")).toBe("image.png")
    expect(putName("", "text/plain")).toBe("upload")
    expect(isVisionFile({ name: "notes.txt", type: "text/plain" })).toBe(false)
    expect(isVisionFile({ name: "shot.png", type: "" })).toBe(true)
    expect(isVisionFile({ name: "notes.txt", type: "image/png" })).toBe(true)
  })
})
