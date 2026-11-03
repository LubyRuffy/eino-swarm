import { MAX_PREVIEW_CELLS } from "./limits"

export type CellGrid = {
  rows: string[][]
  truncated: boolean
  shown: number
  total: number
}

/** RFC-style delimiter split. Quotes can contain the delimiter and newlines.
 * A trailing newline does not invent an extra row. */
export function parseDelimited(text: string, delimiter: "," | "\t"): string[][] {
  const source = text.charCodeAt(0) === 0xfeff ? text.slice(1) : text
  const rows: string[][] = []
  let row: string[] = []
  let cell = ""
  let quoted = false
  for (let index = 0; index < source.length; index++) {
    const ch = source[index]
    if (quoted) {
      if (ch === '"') {
        if (source[index + 1] === '"') {
          cell += '"'
          index++
        } else quoted = false
      } else cell += ch
      continue
    }
    if (ch === '"' && cell === "") {
      quoted = true
      continue
    }
    if (ch === delimiter) {
      row.push(cell)
      cell = ""
      continue
    }
    if (ch === "\n" || ch === "\r") {
      if (ch === "\r" && source[index + 1] === "\n") index++
      row.push(cell)
      rows.push(row)
      row = []
      cell = ""
      continue
    }
    cell += ch
  }
  if (cell !== "" || row.length > 0) {
    row.push(cell)
    rows.push(row)
  }
  while (rows.length > 0 && rows[rows.length - 1].length === 1 && rows[rows.length - 1][0] === "") rows.pop()
  return rows
}

export function limitCells(rows: string[][]): CellGrid {
  let total = 0
  for (const row of rows) total += Math.max(row.length, 1)
  if (total <= MAX_PREVIEW_CELLS) return { rows, truncated: false, shown: total, total }
  const kept: string[][] = []
  let shown = 0
  for (const row of rows) {
    const width = Math.max(row.length, 1)
    if (shown + width > MAX_PREVIEW_CELLS) break
    kept.push(row)
    shown += width
  }
  return { rows: kept, truncated: true, shown, total }
}
