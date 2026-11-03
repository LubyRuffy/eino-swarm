import { limitCells, parseDelimited } from "./delimited"
import { MAX_PREVIEW_CELLS } from "./limits"

describe("delimited preview", () => {
  it("keeps quoted commas, quotes and newlines", () => {
    expect(parseDelimited('Name,Note\r\n"A, B","say ""hi"""\n"line\n2",ok\n', ",")).toEqual([
      ["Name", "Note"],
      ["A, B", 'say "hi"'],
      ["line\n2", "ok"],
    ])
  })

  it("splits tabs and ignores a BOM", () => {
    expect(parseDelimited("\uFEFFa\tb\n1\t2\n", "\t")).toEqual([["a", "b"], ["1", "2"]])
  })

  it("stops the grid after the cell budget and reports the remainder", () => {
    const rows = Array.from({ length: MAX_PREVIEW_CELLS + 3 }, (_, index) => [String(index)])
    const grid = limitCells(rows)
    expect(grid.truncated).toBe(true)
    expect(grid.rows).toHaveLength(MAX_PREVIEW_CELLS)
    expect(grid.total).toBe(MAX_PREVIEW_CELLS + 3)
    expect(grid.shown).toBe(MAX_PREVIEW_CELLS)
  })
})
