import { MAX_PREVIEW_CELLS, MAX_SHEET_COLS, MAX_SHEET_ROWS } from "./limits"
import { relationPath, relationshipId, relationships } from "./relations"
import type { WorkbookPreview } from "./types"
import { readZip, zipText } from "./zip"
import { elementsByLocal, parseXml } from "./xml"

function sharedStrings(xml: string | null): string[] {
  if (!xml) return []
  return elementsByLocal(parseXml(xml), "si").map((item) => elementsByLocal(item, "t").map((text) => text.textContent ?? "").join(""))
}

function columnIndex(letters: string): number {
  let value = 0
  for (const char of letters) value = value * 26 + (char.charCodeAt(0) - 64)
  return value - 1
}

function cellRef(ref: string): { row: number; col: number } | null {
  const match = /^([A-Z]+)(\d+)$/.exec(ref)
  if (!match) return null
  return { col: columnIndex(match[1]), row: Number(match[2]) - 1 }
}

function cellText(cell: Element, strings: string[]): string {
  const type = cell.getAttribute("t") ?? ""
  if (type === "inlineStr") return elementsByLocal(cell, "t").map((text) => text.textContent ?? "").join("")
  const value = elementsByLocal(cell, "v")[0]?.textContent ?? ""
  if (type === "s") return strings[Number(value)] ?? ""
  if (type === "b") return value === "1" ? "TRUE" : "FALSE"
  if (value) return value
  // A formula without a cached value is shown as text. It is not evaluated.
  const formula = elementsByLocal(cell, "f")[0]?.textContent ?? ""
  if (!formula) return ""
  return formula.startsWith("=") ? formula : `=${formula}`
}

function parseSheet(xml: string, strings: string[], state: { shown: number; truncated: boolean }): string[][] {
  const grid = new Map<number, Map<number, string>>()
  let maxRow = -1
  let maxCol = -1
  for (const cell of elementsByLocal(parseXml(xml), "c")) {
    const pos = cellRef(cell.getAttribute("r") ?? "")
    if (!pos || pos.row < 0 || pos.col < 0) continue
    if (pos.row >= MAX_SHEET_ROWS || pos.col >= MAX_SHEET_COLS || state.shown >= MAX_PREVIEW_CELLS) {
      state.truncated = true
      continue
    }
    const text = cellText(cell, strings)
    let row = grid.get(pos.row)
    if (!row) {
      row = new Map()
      grid.set(pos.row, row)
    }
    row.set(pos.col, text)
    state.shown += 1
    maxRow = Math.max(maxRow, pos.row)
    maxCol = Math.max(maxCol, pos.col)
  }
  const rows: string[][] = []
  for (let rowIndex = 0; rowIndex <= maxRow; rowIndex++) {
    const line = grid.get(rowIndex)
    const row: string[] = []
    for (let col = 0; col <= maxCol; col++) row.push(line?.get(col) ?? "")
    rows.push(row)
  }
  while (rows.length > 0 && rows[rows.length - 1].every((cell) => cell === "")) rows.pop()
  return rows
}

export async function parseXlsx(bytes: Uint8Array): Promise<WorkbookPreview> {
  const files = await readZip(bytes)
  const book = zipText(files, "xl/workbook.xml")
  if (!book) throw new Error("workbook is missing")
  const rels = relationships(zipText(files, "xl/_rels/workbook.xml.rels") ?? "")
  const strings = sharedStrings(zipText(files, "xl/sharedStrings.xml"))
  const state = { shown: 0, truncated: false }
  const sheets: { name: string; rows: string[][] }[] = []
  for (const sheet of elementsByLocal(parseXml(book), "sheet")) {
    const rel = rels.get(relationshipId(sheet))
    const path = relationPath("xl/workbook.xml", rel)
    const xml = path ? zipText(files, path) : null
    if (!xml) continue
    const name = sheet.getAttribute("name") || `Sheet ${sheets.length + 1}`
    sheets.push({ name, rows: parseSheet(xml, strings, state) })
    if (state.truncated) break
  }
  if (sheets.length === 0) throw new Error("workbook has no sheets")
  return { sheets, truncated: state.truncated, shown: state.shown }
}
