/** Same parser as the desktop transcript. A `chart` markdown fence is JSON the answer already committed to. The
 *  renderer has to accept the shapes models actually emit — missing x/y,
 *  parallel arrays, x/y used as captions while the row uses other keys —
 *  without inventing series that were never in the body. */

export const CHART_TYPES = ["bar", "line", "area", "pie"] as const
export type ChartType = (typeof CHART_TYPES)[number]

export type ChartRow = Record<string, string | number>

export type ChartSpec = {
  type: ChartType
  title: string
  unit: string
  x: string
  y: string[]
  stacked: boolean
  data: ChartRow[]
}

export type ChartParseResult =
  | { ok: true; spec: ChartSpec }
  | { ok: false; incomplete: boolean }

export const CHART_MAX_ROWS = 48
export const CHART_MAX_SERIES = 8
export const CHART_MIN_ROWS = 2

/** True when two parsed specs would paint the same figure. Streaming
 *  re-parses the fence on every token and must not treat a new object as
 *  a new chart. */
export function chartSpecsEqual(a: ChartSpec, b: ChartSpec): boolean {
  if (a === b) return true
  if (
    a.type !== b.type ||
    a.title !== b.title ||
    a.unit !== b.unit ||
    a.x !== b.x ||
    a.stacked !== b.stacked ||
    a.y.length !== b.y.length ||
    a.data.length !== b.data.length
  ) {
    return false
  }
  for (let i = 0; i < a.y.length; i++) {
    if (a.y[i] !== b.y[i]) return false
  }
  for (let i = 0; i < a.data.length; i++) {
    const left = a.data[i]
    const right = b.data[i]
    if (left[a.x] !== right[a.x]) return false
    for (const key of a.y) {
      if (left[key] !== right[key]) return false
    }
  }
  return true
}

export function parseChartSpec(raw: string): ChartParseResult {
  const text = raw.trim()
  if (!text) return { ok: false, incomplete: true }
  let value: unknown
  try {
    value = JSON.parse(text)
  } catch {
    return { ok: false, incomplete: looksIncomplete(text) }
  }
  if (!isRecord(value)) return { ok: false, incomplete: false }
  const spec = normalizeChart(value)
  if (!spec) return { ok: false, incomplete: false }
  return { ok: true, spec }
}

function looksIncomplete(text: string): boolean {
  if (!text.startsWith("{")) return false
  let depth = 0
  let inString = false
  let escape = false
  for (const ch of text) {
    if (inString) {
      if (escape) {
        escape = false
        continue
      }
      if (ch === "\\") {
        escape = true
        continue
      }
      if (ch === '"') inString = false
      continue
    }
    if (ch === '"') {
      inString = true
      continue
    }
    if (ch === "{") depth++
    else if (ch === "}") depth--
  }
  return inString || depth > 0 || /,\s*$/.test(text)
}

function normalizeChart(value: Record<string, unknown>): ChartSpec | null {
  const type = readType(value)
  if (!type) return null
  const title = readString(value, "title") || readString(value, "name")
  const unit = readString(value, "unit")
  const stacked = value.stacked === true
  const rows = readRows(value)
  if (!rows) return null

  const bound = bindChartColumns(rows, readString(value, "x"), readY(value.y))
  if (!bound) return null
  const x = bound.x
  let y = unique(bound.y).slice(0, CHART_MAX_SERIES)
  if (type === "pie") y = y.slice(0, 1)

  const data: ChartRow[] = []
  for (const row of bound.rows) {
    if (data.length >= CHART_MAX_ROWS) break
    const cleaned = cleanRow(row, x, y)
    if (cleaned) data.push(cleaned)
  }
  if (data.length < CHART_MIN_ROWS) return null
  return { type, title, unit, x, y, stacked, data }
}

function readType(value: Record<string, unknown>): ChartType | null {
  const raw = readString(value, "type") || readString(value, "kind")
  const t = raw.toLowerCase()
  return (CHART_TYPES as readonly string[]).includes(t) ? (t as ChartType) : null
}

function readY(raw: unknown): string[] {
  if (typeof raw === "string" && raw.trim()) return [raw.trim()]
  if (!Array.isArray(raw)) return []
  return raw
    .filter((v): v is string => typeof v === "string" && v.trim() !== "")
    .map((v) => v.trim())
}

function readRows(value: Record<string, unknown>): Record<string, unknown>[] | null {
  const fromData = asObjectArray(value.data) ?? asObjectArray(value.points)
  if (fromData) return fromData
  const fromPairs = rowsFromLabels(value)
  if (fromPairs) return fromPairs
  const fromMap = rowsFromValueMap(value.values)
  if (fromMap) return fromMap
  return null
}

function asObjectArray(raw: unknown): Record<string, unknown>[] | null {
  if (!Array.isArray(raw) || raw.length === 0) return null
  if (!raw.every(isRecord)) return null
  return raw
}

function rowsFromLabels(value: Record<string, unknown>): Record<string, unknown>[] | null {
  if (!Array.isArray(value.labels) || !Array.isArray(value.values)) return null
  if (value.labels.length !== value.values.length || value.labels.length === 0) {
    return null
  }
  const rows: Record<string, unknown>[] = []
  for (let i = 0; i < value.labels.length; i++) {
    const n = asNumber(value.values[i])
    if (n === null) return null
    rows.push({ label: stringify(value.labels[i]), value: n })
  }
  return rows
}

function rowsFromValueMap(raw: unknown): Record<string, unknown>[] | null {
  if (!isRecord(raw)) return null
  const rows: Record<string, unknown>[] = []
  for (const [k, v] of Object.entries(raw)) {
    const n = asNumber(v)
    if (n === null) return null
    rows.push({ label: k, value: n })
  }
  return rows.length ? rows : null
}

/** Declared x/y are field names when those keys exist. A newer model writes
 *  the axis caption there and stores the point under the literal keys `x`
 *  and `y`, or under some other pair. Binding keeps the columns that are
 *  actually on the row and only renames a placeholder key onto that caption. */
function bindChartColumns(
  rows: Record<string, unknown>[],
  declaredX: string,
  declaredY: string[],
): { x: string; y: string[]; rows: Record<string, unknown>[] } | null {
  const candidates: { x: string; y: string[]; rows: Record<string, unknown>[] }[] = []
  if (declaredX && declaredY.length > 0) {
    candidates.push({ x: declaredX, y: declaredY, rows })
  }
  const yHit = declaredY.filter((key) => columnIsNumeric(rows, key))
  if (yHit.length > 0) {
    const partial = inferAxes(rows, "", yHit)
    if (partial) candidates.push(withCaptions(rows, partial, declaredX, declaredY))
  }
  const full = inferAxes(rows, "", [])
  if (full) candidates.push(withCaptions(rows, full, declaredX, declaredY))
  for (const candidate of candidates) {
    if (enoughRows(candidate.rows, candidate.x, candidate.y)) return candidate
  }
  return null
}

function withCaptions(
  rows: Record<string, unknown>[],
  inferred: { x: string; y: string[] },
  captionX: string,
  captionY: string[],
): { x: string; y: string[]; rows: Record<string, unknown>[] } {
  const renames: Record<string, string> = {}
  if (
    inferred.x === "x" &&
    captionX &&
    captionX !== "x" &&
    !columnExists(rows, captionX)
  ) {
    renames.x = captionX
  }
  const yCaption = captionY.length === 1 ? captionY[0] : ""
  if (
    inferred.y.length === 1 &&
    inferred.y[0] === "y" &&
    yCaption &&
    yCaption !== "y" &&
    !columnExists(rows, yCaption)
  ) {
    renames.y = yCaption
  }
  if (Object.keys(renames).length === 0) return { ...inferred, rows }
  const x = renames.x ?? inferred.x
  const y = inferred.y.map((key) => renames[key] ?? key)
  if (y.includes(x)) return { ...inferred, rows }
  return { x, y, rows: renameKeys(rows, renames) }
}

function renameKeys(
  rows: Record<string, unknown>[],
  renames: Record<string, string>,
): Record<string, unknown>[] {
  return rows.map((row) => {
    const out: Record<string, unknown> = {}
    for (const [key, value] of Object.entries(row)) {
      out[renames[key] ?? key] = value
    }
    return out
  })
}

function enoughRows(
  rows: Record<string, unknown>[],
  x: string,
  y: string[],
): boolean {
  if (!x || y.length === 0) return false
  let n = 0
  for (const row of rows) {
    if (!cleanRow(row, x, y)) continue
    n++
    if (n >= CHART_MIN_ROWS) return true
  }
  return false
}

function columnIsNumeric(rows: Record<string, unknown>[], key: string): boolean {
  return rows.every((row) => asNumber(row[key]) !== null)
}

function columnExists(rows: Record<string, unknown>[], key: string): boolean {
  return rows.some((row) => Object.prototype.hasOwnProperty.call(row, key))
}

function inferAxes(
  rows: Record<string, unknown>[],
  x: string,
  y: string[],
): { x: string; y: string[] } | null {
  const keys = Object.keys(rows[0] ?? {})
  if (keys.length === 0) return null
  const numeric = keys.filter((k) => rows.every((row) => asNumber(row[k]) !== null))
  const categorical = keys.filter((k) => !numeric.includes(k))
  const nextX = x || categorical[0] || ""
  const nextY = y.length > 0 ? y : numeric.filter((k) => k !== nextX)
  if (!nextX || nextY.length === 0) return null
  return { x: nextX, y: nextY }
}

function cleanRow(row: Record<string, unknown>, x: string, y: string[]): ChartRow | null {
  if (!(x in row)) return null
  const label = stringify(row[x])
  if (!label) return null
  const out: ChartRow = { [x]: label }
  for (const key of y) {
    const n = asNumber(row[key])
    if (n === null) return null
    out[key] = n
  }
  return out
}

function readString(value: Record<string, unknown>, key: string): string {
  const v = value[key]
  return typeof v === "string" ? v.trim() : ""
}

function stringify(v: unknown): string {
  if (typeof v === "string") return v.trim()
  if (typeof v === "number" && Number.isFinite(v)) return String(v)
  if (typeof v === "boolean") return v ? "true" : "false"
  return ""
}

function asNumber(v: unknown): number | null {
  if (typeof v === "number" && Number.isFinite(v)) return v
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v)
    if (Number.isFinite(n)) return n
  }
  return null
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

function unique(items: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of items) {
    if (seen.has(item)) continue
    seen.add(item)
    out.push(item)
  }
  return out
}
