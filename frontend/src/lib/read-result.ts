/** The paging header eino-tools `read` prefixes onto every file body. */
const HEADER =
  /^encoding=(\S+) path=(\S+) offset=(\d+) limit=(\d+)(?:\n| |\r\n)/

export interface ReadLine {
  n: number
  text: string
}

export interface ReadListing {
  encoding: string
  path: string
  offset: number
  limit: number
  lines: ReadLine[]
  body: string
}

/** Turn a `read` tool payload into a file listing. Understands both the live
 *  newline-separated form and the older one-line dump that collapsed `\n`
 *  into spaces — expanding those rows still has to look like a file. */
export function parseReadResult(raw: string): ReadListing | undefined {
  const m = HEADER.exec(raw)
  if (!m) return undefined
  const rest = raw.slice(m[0].length)
  const lines = rest.trim() === "" ? [] : splitNumberedLines(rest)
  if (!lines) return undefined
  return {
    encoding: m[1],
    path: m[2],
    offset: Number(m[3]),
    limit: Number(m[4]),
    lines,
    body: lines.map((l) => l.text).join("\n"),
  }
}

export function isMarkdownPath(path: string): boolean {
  return /\.(md|markdown|mdx)$/i.test(path)
}

function splitNumberedLines(rest: string): ReadLine[] | undefined {
  const matches: { n: number; start: number; end: number }[] = []
  const re = /(?:^|\n|\r\n| )(\d+)\|/g
  let m: RegExpExecArray | null
  while ((m = re.exec(rest))) {
    matches.push({ n: Number(m[1]), start: m.index, end: m.index + m[0].length })
  }
  if (matches.length === 0) return undefined
  return matches.map((cur, i) => {
    const to = i + 1 < matches.length ? matches[i + 1].start : rest.length
    return { n: cur.n, text: rest.slice(cur.end, to).replace(/[\r\n]+$/, "") }
  })
}
