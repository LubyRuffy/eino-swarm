import { zipStore } from "./zip-store"
import { readZip, safeZipName, ZipError } from "./zip"

const png = Uint8Array.from(atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jWZkAAAAASUVORK5CYII="), (char) => char.charCodeAt(0))

describe("zip preview archives", () => {
  it("reads stored names and bytes", async () => {
    const files = await readZip(zipStore([
      { name: "word/document.xml", data: "<w:document/>" },
      { name: "word/media/pixel.png", data: png },
    ]))
    expect(new TextDecoder().decode(files.get("word/document.xml"))).toBe("<w:document/>")
    expect(files.get("word/media/pixel.png")).toEqual(png)
  })

  it("reads a deflate entry", async () => {
    const { deflateRawSync } = await import("node:zlib")
    const raw = new TextEncoder().encode("hello preview")
    const compressed = deflateRawSync(raw)
    const stored = zipStore([{ name: "note.txt", data: raw }])
    const eocd = stored.length - 22
    const view = new DataView(stored.buffer)
    const nameAt = 30
    const dataAt = nameAt + "note.txt".length
    const rebuilt = new Uint8Array(stored.byteLength - raw.length + compressed.length)
    rebuilt.set(stored.subarray(0, dataAt), 0)
    rebuilt.set(compressed, dataAt)
    const directoryShift = compressed.length - raw.length
    rebuilt.set(stored.subarray(dataAt + raw.length), dataAt + compressed.length)
    const local = new DataView(rebuilt.buffer)
    local.setUint16(8, 8, true)
    local.setUint32(18, compressed.length, true)
    const cd = dataAt + compressed.length
    local.setUint16(cd + 10, 8, true)
    local.setUint32(cd + 20, compressed.length, true)
    local.setUint32(rebuilt.length - 6, view.getUint32(eocd + 16, true) + directoryShift, true)
    expect(new TextDecoder().decode((await readZip(rebuilt)).get("note.txt"))).toBe("hello preview")
  })

  it("drops names that leave the archive and rejects declared bombs", async () => {
    expect(safeZipName("../secret.txt")).toBeNull()
    expect(safeZipName("ok/../kept.txt")).toBeNull()
    const files = await readZip(zipStore([{ name: "../secret.txt", data: "no" }, { name: "kept.txt", data: "yes" }]))
    expect(files.has("../secret.txt")).toBe(false)
    expect(new TextDecoder().decode(files.get("kept.txt"))).toBe("yes")
    const bomb = zipStore([{ name: "big.txt", data: "x" }])
    const patch = new DataView(bomb.buffer)
    patch.setUint32(22, 32 * 1024 * 1024 + 1, true)
    const cd = 30 + "big.txt".length + 1
    patch.setUint32(cd + 24, 32 * 1024 * 1024 + 1, true)
    await expect(readZip(bomb)).rejects.toBeInstanceOf(ZipError)
  })

  it("rejects an entry that expands past its declared size", async () => {
    const { deflateRawSync } = await import("node:zlib")
    const raw = new TextEncoder().encode("hello preview")
    const compressed = deflateRawSync(raw)
    const stored = zipStore([{ name: "note.txt", data: raw }])
    const eocd = stored.length - 22
    const view = new DataView(stored.buffer)
    const dataAt = 30 + "note.txt".length
    const rebuilt = new Uint8Array(stored.byteLength - raw.length + compressed.length)
    rebuilt.set(stored.subarray(0, dataAt), 0)
    rebuilt.set(compressed, dataAt)
    const directoryShift = compressed.length - raw.length
    rebuilt.set(stored.subarray(dataAt + raw.length), dataAt + compressed.length)
    const local = new DataView(rebuilt.buffer)
    local.setUint16(8, 8, true)
    local.setUint32(18, compressed.length, true)
    local.setUint32(22, 0, true)
    const cd = dataAt + compressed.length
    local.setUint16(cd + 10, 8, true)
    local.setUint32(cd + 20, compressed.length, true)
    local.setUint32(cd + 24, 0, true)
    local.setUint32(rebuilt.length - 6, view.getUint32(eocd + 16, true) + directoryShift, true)
    await expect(readZip(rebuilt)).rejects.toBeInstanceOf(ZipError)

    const storedLie = zipStore([{ name: "big.txt", data: "x" }])
    const patch = new DataView(storedLie.buffer)
    patch.setUint32(22, 0, true)
    patch.setUint32(30 + "big.txt".length + 1 + 24, 0, true)
    await expect(readZip(storedLie)).rejects.toBeInstanceOf(ZipError)
  })

  it("rejects a truncated archive", async () => {
    await expect(readZip(new Uint8Array([1, 2, 3]))).rejects.toBeInstanceOf(ZipError)
  })
})
