/** Interpret CR the way a terminal does: return to column 0 of the current
 *  line and overwrite. Progress printers would otherwise dump a megabyte of
 *  `\r` junk into a <pre>. */
export function applyCarriageReturns(text: string): string {
  const out: string[] = []
  let line = ""
  let col = 0
  for (const ch of text) {
    if (ch === "\n") {
      out.push(line)
      line = ""
      col = 0
      continue
    }
    if (ch === "\r") {
      col = 0
      continue
    }
    if (col < line.length) {
      line = line.slice(0, col) + ch + line.slice(col + 1)
    } else {
      line += ch
    }
    col++
  }
  out.push(line)
  return out.join("\n")
}

export function lastLine(text: string): string {
  const lines = text.split("\n")
  for (let i = lines.length - 1; i >= 0; i--) {
    const line = lines[i].trim()
    if (line) return line
  }
  return ""
}
