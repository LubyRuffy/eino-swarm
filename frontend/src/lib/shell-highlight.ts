/** Shell tokens for an exec line. The renderer maps these onto CSS variables;
 *  concatenating `.text` must equal the original command so expand-to-read
 *  never loses characters. */

export type ShellTokenKind =
  | "text"
  | "comment"
  | "string"
  | "keyword"
  | "command"
  | "operator"
  | "flag"
  | "variable"

export type ShellToken = { kind: ShellTokenKind; text: string }

/** A single line longer than this is painted as plain text. Syntect-style
 *  highlighters stall on a 10k-char one-liner; a command that size is still
 *  shown, just not coloured. */
const MAX_LINE = 4 * 1024

const KEYWORDS = new Set([
  "if",
  "then",
  "else",
  "elif",
  "fi",
  "for",
  "while",
  "until",
  "do",
  "done",
  "case",
  "esac",
  "in",
  "function",
  "select",
  "time",
  "coproc",
])

/** After these, the next word is a command (`then echo`), not an argument. */
const NEXT_IS_COMMAND = new Set(["then", "do", "else", "elif", "if", "while", "until"])

const OPERATORS = [
  ";;&",
  "||",
  "&&",
  "|&",
  ">>",
  "<<",
  ">&",
  "<&",
  "&>",
  ">|",
  ";;",
  ";&",
  "|",
  ";",
  "&",
  ">",
  "<",
  "(",
  ")",
  "{",
  "}",
  "!",
]

const STMT_OPS = new Set(["&&", "||", "|&", "|", ";", "&", "(", "{", ";;", ";&", ";;&", "!"])

export function tokenizeShell(src: string): ShellToken[] {
  if (!src) return []
  if (src.split("\n").some((line) => line.length > MAX_LINE)) {
    return [{ kind: "text", text: src }]
  }
  const tokens: ShellToken[] = []
  let i = 0
  let stmt = true
  while (i < src.length) {
    const c = src[i]
    if (c === " " || c === "\t") {
      const start = i
      while (i < src.length && (src[i] === " " || src[i] === "\t")) i++
      push(tokens, "text", src.slice(start, i))
      continue
    }
    if (c === "\n") {
      push(tokens, "text", "\n")
      i++
      stmt = true
      continue
    }
    if (c === "#") {
      const start = i
      while (i < src.length && src[i] !== "\n") i++
      push(tokens, "comment", src.slice(start, i))
      continue
    }
    if (c === "'" || c === '"') {
      const end = readQuote(src, i)
      push(tokens, "string", src.slice(i, end))
      i = end
      stmt = false
      continue
    }
    if (src.startsWith("<<", i)) {
      i = readHeredoc(src, i, tokens)
      stmt = true
      continue
    }
    if (src.startsWith("$(", i)) {
      push(tokens, "operator", "$(")
      i += 2
      const close = matchingParen(src, i)
      const inner = tokenizeShell(src.slice(i, close))
      for (const t of inner) push(tokens, t.kind, t.text)
      if (close < src.length && src[close] === ")") {
        push(tokens, "operator", ")")
        i = close + 1
      } else {
        i = close
      }
      stmt = false
      continue
    }
    if (c === "$") {
      const end = readVariable(src, i)
      push(tokens, "variable", src.slice(i, end))
      i = end
      stmt = false
      continue
    }
    const fd = readFdRedirect(src, i)
    if (fd) {
      push(tokens, "operator", src.slice(i, fd))
      i = fd
      stmt = true
      continue
    }
    const op = matchOperator(src, i)
    if (op) {
      push(tokens, "operator", op)
      i += op.length
      if (STMT_OPS.has(op)) stmt = true
      else stmt = false
      continue
    }
    if (c === "-" && (src[i + 1] === "-" || isWordChar(src[i + 1]))) {
      const start = i
      i++
      if (src[i] === "-") i++
      while (i < src.length && /[A-Za-z0-9_=-]/.test(src[i])) i++
      push(tokens, "flag", src.slice(start, i))
      stmt = false
      continue
    }
    const start = i
    while (i < src.length && !isSeparator(src, i)) i++
    const word = src.slice(start, i)
    if (!word) {
      push(tokens, "text", c)
      i++
      continue
    }
    if (stmt && KEYWORDS.has(word)) {
      push(tokens, "keyword", word)
      stmt = NEXT_IS_COMMAND.has(word)
      continue
    }
    if (stmt) {
      push(tokens, "command", word)
      stmt = false
      continue
    }
    if (KEYWORDS.has(word)) {
      push(tokens, "keyword", word)
      stmt = word === "in" ? false : stmt
      continue
    }
    push(tokens, "text", word)
  }
  return tokens
}

function push(tokens: ShellToken[], kind: ShellTokenKind, text: string) {
  if (!text) return
  const last = tokens[tokens.length - 1]
  if (last && last.kind === kind) {
    last.text += text
    return
  }
  tokens.push({ kind, text })
}

function isWordChar(c: string | undefined): boolean {
  return Boolean(c && /[A-Za-z0-9_]/.test(c))
}

function isSeparator(src: string, i: number): boolean {
  const c = src[i]
  if (!c || c === " " || c === "\t" || c === "\n" || c === "#") return true
  if (c === "'" || c === '"') return true
  if (c === "$") return true
  if (matchOperator(src, i)) return true
  if (src.startsWith("<<", i)) return true
  return false
}

function matchOperator(src: string, i: number): string | undefined {
  for (const op of OPERATORS) {
    if (src.startsWith(op, i)) return op
  }
  return undefined
}

function readQuote(src: string, i: number): number {
  const q = src[i]
  i++
  while (i < src.length) {
    if (src[i] === "\\" && q === '"') {
      i += 2
      continue
    }
    if (src[i] === q) return i + 1
    i++
  }
  return src.length
}

function readVariable(src: string, i: number): number {
  // ${...} or $NAME or $1
  if (src[i + 1] === "{") {
    let j = i + 2
    let depth = 1
    while (j < src.length && depth > 0) {
      if (src[j] === "{") depth++
      else if (src[j] === "}") depth--
      j++
    }
    return j
  }
  let j = i + 1
  if (j < src.length && /\d/.test(src[j])) return j + 1
  while (j < src.length && /[A-Za-z0-9_]/.test(src[j])) j++
  return j === i + 1 ? i + 1 : j
}

function readFdRedirect(src: string, i: number): number | undefined {
  if (!/\d/.test(src[i])) return undefined
  let j = i
  while (j < src.length && /\d/.test(src[j])) j++
  const op = matchOperator(src, j)
  if (!op || (op[0] !== ">" && op[0] !== "<")) return undefined
  return j + op.length
}

function matchingParen(src: string, i: number): number {
  let depth = 1
  while (i < src.length && depth > 0) {
    if (src[i] === "'" || src[i] === '"') {
      i = readQuote(src, i)
      continue
    }
    if (src[i] === "(") depth++
    else if (src[i] === ")") {
      depth--
      if (depth === 0) return i
    }
    i++
  }
  return src.length
}

function readHeredoc(src: string, i: number, tokens: ShellToken[]): number {
  const opStart = i
  i += 2
  if (src[i] === "-") i++
  push(tokens, "operator", src.slice(opStart, i))
  const ws = i
  while (i < src.length && (src[i] === " " || src[i] === "\t")) i++
  if (i > ws) push(tokens, "text", src.slice(ws, i))

  let quote = ""
  if (src[i] === "'" || src[i] === '"') {
    quote = src[i]
    i++
  }
  const d0 = i
  while (i < src.length && src[i] !== "\n" && src[i] !== quote && src[i] !== " " && src[i] !== "\t") {
    i++
  }
  const delim = src.slice(d0, i)
  if (quote && src[i] === quote) i++
  push(tokens, "string", src.slice(quote ? d0 - 1 : d0, i))
  if (!delim) return i

  const restStart = i
  const rest = src.slice(i)
  const nl = rest.indexOf("\n")
  if (nl < 0) {
    if (rest) push(tokens, "text", rest)
    return src.length
  }
  if (nl > 0) push(tokens, "text", rest.slice(0, nl))
  push(tokens, "text", "\n")
  const body = rest.slice(nl + 1)
  const closer = new RegExp(`^${escapeRe(delim)}$`, "m")
  const m = closer.exec(body)
  if (!m || m.index === undefined) {
    push(tokens, "string", body)
    return src.length
  }
  const bodyText = body.slice(0, m.index)
  const closerText = body.slice(m.index, m.index + delim.length)
  if (bodyText) push(tokens, "string", bodyText)
  push(tokens, "string", closerText)
  return restStart + 1 + nl + 1 + m.index + delim.length
}

function escapeRe(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")
}
