/** Reconstruct an `edit` / `write` as a line hunk. The tool result is only a
 *  status sentence (`ok: replaced block in …` / `Updated file <abs> (N bytes)`); the
 *  change itself is in the args. */

export type DiffOp = "eq" | "add" | "del"

export type DiffLine = {
  op: DiffOp
  text: string
}

export type EditHunk = {
  header?: string
  lines: DiffLine[]
}

export type EditDiff = {
  path: string
  hunks: EditHunk[]
  added: number
  removed: number
}

/** A 400×500 middle hunk is still a UI; past that, prefix/suffix then a
 *  dumb del-then-add, because an O(nm) table would freeze the transcript. */
const LCS_CELLS = 200_000

export function parseEditDiff(args: string): EditDiff | undefined {
  const obj = parseObject(args)
  if (!obj) return undefined
  const path = asPath(obj.file_path)
  const search = asRaw(obj.search_block)
  const replace = asRaw(obj.replace_block)
  const patch = asRaw(obj.patch)
  const hunks =
    search !== "" || replace !== ""
      ? [{ lines: lineDiff(splitLines(search), splitLines(replace)) }]
      : patch
        ? parsePatch(patch)
        : undefined
  if (!hunks?.length) return undefined
  const lines = hunks.flatMap((h) => h.lines)
  if (!lines.length) return undefined
  let added = 0
  let removed = 0
  for (const line of lines) {
    if (line.op === "add") added++
    else if (line.op === "del") removed++
  }
  return { path, hunks, added, removed }
}

/** A `write` has no before-image in the args, so the body is all additions.
 *  Empty `content` still counts: they created the file. Missing `content` is
 *  not a write we can show. */
export function parseWriteDiff(args: string): EditDiff | undefined {
  const obj = parseObject(args)
  if (!obj || typeof obj.content !== "string") return undefined
  const path = asPath(obj.file_path)
  const lines = splitLines(obj.content).map((text) => ({ op: "add" as const, text }))
  return { path, hunks: [{ lines }], added: lines.length, removed: 0 }
}

export function parseFileChange(name: string, args: string): EditDiff | undefined {
  if (name === "edit") return parseEditDiff(args)
  if (name === "write") return parseWriteDiff(args)
  return undefined
}

export function editCountLabel(diff: Pick<EditDiff, "added" | "removed">): string {
  if (diff.removed === 0) return `+${diff.added}`
  if (diff.added === 0) return `−${diff.removed}`
  return `+${diff.added} −${diff.removed}`
}

/** A whole-file write of thousands of lines must not mount thousands of
 *  DOM rows. The header still reports the real +N −M. */
export const DIFF_LINE_CAP = 400

export function clipDiff(diff: EditDiff, cap = DIFF_LINE_CAP): { hunks: EditHunk[]; hidden: number } {
  let used = 0
  const hunks: EditHunk[] = []
  for (const hunk of diff.hunks) {
    if (used >= cap) break
    const room = cap - used
    if (hunk.lines.length <= room) {
      hunks.push(hunk)
      used += hunk.lines.length
      continue
    }
    hunks.push({ header: hunk.header, lines: hunk.lines.slice(0, room) })
    used += room
    break
  }
  const total = diff.hunks.reduce((n, h) => n + h.lines.length, 0)
  return { hunks, hidden: Math.max(0, total - used) }
}

function parsePatch(text: string): EditHunk[] | undefined {
  const hunks: EditHunk[] = []
  let lines: DiffLine[] | undefined
  let header: string | undefined
  const flush = () => {
    if (lines?.length) hunks.push({ header, lines })
    lines = undefined
    header = undefined
  }
  for (const line of splitLines(text)) {
    if (isPatchMeta(line)) continue
    if (line.startsWith("@@")) {
      flush()
      lines = []
      header = line
      continue
    }
    const parsed = parsePatchLine(line)
    if (!parsed) continue
    if (!lines) lines = []
    lines.push(parsed)
  }
  flush()
  return hunks.length ? hunks : undefined
}

function isPatchMeta(line: string): boolean {
  return (
    line.startsWith("*** ") ||
    line.startsWith("--- ") ||
    line.startsWith("+++ ") ||
    line === "\\ No newline at end of file"
  )
}

function parsePatchLine(line: string): DiffLine | undefined {
  if (line === "") return { op: "eq", text: "" }
  const mark = line[0]
  const text = line.slice(1)
  if (mark === "+") return { op: "add", text }
  if (mark === "-") return { op: "del", text }
  if (mark === " ") return { op: "eq", text }
  return undefined
}

function lineDiff(a: string[], b: string[]): DiffLine[] {
  let start = 0
  while (start < a.length && start < b.length && a[start] === b[start]) start++
  let aEnd = a.length
  let bEnd = b.length
  while (aEnd > start && bEnd > start && a[aEnd - 1] === b[bEnd - 1]) {
    aEnd--
    bEnd--
  }
  const prefix = a.slice(0, start).map((text) => ({ op: "eq" as const, text }))
  const suffix = a.slice(aEnd).map((text) => ({ op: "eq" as const, text }))
  return [...prefix, ...lcsDiff(a.slice(start, aEnd), b.slice(start, bEnd)), ...suffix]
}

function lcsDiff(a: string[], b: string[]): DiffLine[] {
  const n = a.length
  const m = b.length
  if (!n) return b.map((text) => ({ op: "add" as const, text }))
  if (!m) return a.map((text) => ({ op: "del" as const, text }))
  if (n * m > LCS_CELLS) {
    return [
      ...a.map((text) => ({ op: "del" as const, text })),
      ...b.map((text) => ({ op: "add" as const, text })),
    ]
  }
  const w = m + 1
  const dp = new Int32Array((n + 1) * w)
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      dp[i * w + j] =
        a[i] === b[j]
          ? dp[(i + 1) * w + (j + 1)] + 1
          : Math.max(dp[(i + 1) * w + j], dp[i * w + (j + 1)])
    }
  }
  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ op: "eq", text: a[i] })
      i++
      j++
    } else if (dp[(i + 1) * w + j] >= dp[i * w + (j + 1)]) {
      out.push({ op: "del", text: a[i] })
      i++
    } else {
      out.push({ op: "add", text: b[j] })
      j++
    }
  }
  while (i < n) {
    out.push({ op: "del", text: a[i++] })
  }
  while (j < m) {
    out.push({ op: "add", text: b[j++] })
  }
  return out
}

function splitLines(text: string): string[] {
  const n = text.replace(/\r\n/g, "\n").replace(/\r/g, "\n")
  if (n === "") return []
  const body = n.endsWith("\n") ? n.slice(0, -1) : n
  return body.split("\n")
}

function parseObject(raw: string): Record<string, unknown> | undefined {
  try {
    const v = JSON.parse(raw) as unknown
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : undefined
  } catch {
    return undefined
  }
}

function asRaw(v: unknown): string {
  return typeof v === "string" ? v : ""
}

function asPath(v: unknown): string {
  return typeof v === "string" ? v.trim() : ""
}
