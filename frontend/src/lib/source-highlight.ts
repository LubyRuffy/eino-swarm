import type { ReadLine } from "./read-result"
import {
  languageFromFence,
  languageFromPath,
  langDef,
  type LangDef,
  type LangId,
} from "./source-lang"
import { tokenizeShell, type ShellTokenKind } from "./shell-highlight"

/** Same kinds as an exec line so a read of `*.go` and a shell command share
 *  one CSS family. Concatenating `.text` must equal the file body or Find
 *  and copy paint a different document than the one on disk. */
export type SourceTokenKind = ShellTokenKind
export type SourceToken = { kind: SourceTokenKind; text: string }

export { languageFromFence, languageFromPath }

/** A single line longer than this is painted as plain text. Same ceiling as
 *  shell highlighting: a 10k-char one-liner is still shown, just not coloured. */
const MAX_LINE = 4 * 1024

const OPS = [
  "<<=",
  ">>=",
  "...",
  ":=",
  "==",
  "!=",
  "<=",
  ">=",
  "&&",
  "||",
  "<-",
  "+=",
  "-=",
  "*=",
  "/=",
  "%=",
  "&=",
  "|=",
  "^=",
  "<<",
  ">>",
  "++",
  "--",
  "=>",
  "->",
  "::",
  "??",
  "?.",
  "**",
]

export function tokenizeSource(src: string, lang: LangId | string): SourceToken[] {
  if (!src) return []
  if (src.split("\n").some((line) => line.length > MAX_LINE)) {
    return [{ kind: "text", text: src }]
  }
  if (lang === "shell") return tokenizeShell(src)
  if (lang === "html") return tokenizeMarkup(src)
  const def = langDef(lang as LangId)
  if (!def) return [{ kind: "text", text: src }]
  return tokenizeCLike(src, def)
}

/** Split tokens on newlines so each numbered row can keep its own spans.
 *  Empty lines stay empty arrays; the renderer must not invent a space. */
export function tokensByLine(tokens: SourceToken[]): SourceToken[][] {
  const lines: SourceToken[][] = [[]]
  for (const token of tokens) {
    const parts = token.text.split("\n")
    for (let i = 0; i < parts.length; i++) {
      if (i > 0) lines.push([])
      if (parts[i]) lines[lines.length - 1].push({ kind: token.kind, text: parts[i] })
    }
  }
  return lines
}

export function paintLines(
  lines: ReadLine[],
  body: string,
  path: string,
): { n: number; tokens: SourceToken[] }[] {
  const lang = languageFromPath(path)
  if (!lang) {
    return lines.map((line) => ({
      n: line.n,
      tokens: line.text ? [{ kind: "text" as const, text: line.text }] : [],
    }))
  }
  const painted = tokensByLine(tokenizeSource(body, lang))
  if (painted.length !== lines.length) {
    return lines.map((line) => ({
      n: line.n,
      tokens: line.text ? [{ kind: "text" as const, text: line.text }] : [],
    }))
  }
  return lines.map((line, i) => ({ n: line.n, tokens: painted[i] }))
}

function tokenizeCLike(src: string, def: LangDef): SourceToken[] {
  const tokens: SourceToken[] = []
  let i = 0
  let pendingName = false
  if (def.hashbang && src.startsWith("#!")) {
    const nl = src.indexOf("\n")
    const end = nl < 0 ? src.length : nl
    push(tokens, "comment", src.slice(0, end))
    i = end
  }
  while (i < src.length) {
    const c = src[i]
    if (c === " " || c === "\t" || c === "\r" || c === "\n") {
      const start = i
      while (i < src.length && isSpace(src[i])) i++
      push(tokens, "text", src.slice(start, i))
      continue
    }
    if (def.hexColors && c === "#" && isHex(src[i + 1])) {
      const start = i
      i++
      while (i < src.length && isHex(src[i])) i++
      push(tokens, "flag", src.slice(start, i))
      pendingName = false
      continue
    }
    if (def.hashDirective && c === "#") {
      const start = i
      i++
      while (i < src.length && isSpace(src[i]) && src[i] !== "\n") i++
      while (i < src.length && isIdentPart(src[i])) i++
      push(tokens, "keyword", src.slice(start, i))
      pendingName = false
      continue
    }
    if (def.lineComment && src.startsWith(def.lineComment, i)) {
      const start = i
      i += def.lineComment.length
      while (i < src.length && src[i] !== "\n") i++
      push(tokens, "comment", src.slice(start, i))
      pendingName = false
      continue
    }
    if (def.blockComment && src.startsWith(def.blockComment[0], i)) {
      const start = i
      i += def.blockComment[0].length
      const close = def.blockComment[1]
      const found = src.indexOf(close, i)
      i = found < 0 ? src.length : found + close.length
      push(tokens, "comment", src.slice(start, i))
      pendingName = false
      continue
    }
    if (def.dollarIdent && c === "$") {
      const end = readDollarIdent(src, i)
      push(tokens, "variable", src.slice(i, end))
      i = end
      pendingName = false
      continue
    }
    if (c === "'" && looksLikeLifetime(src, i)) {
      const end = readLifetime(src, i)
      push(tokens, "variable", src.slice(i, end))
      i = end
      pendingName = false
      continue
    }
    const prefixAt = def.stringPrefixes ? stringPrefixEnd(src, i) : i
    if (def.tripleQuotes && isTriple(src, prefixAt)) {
      const quote = src.slice(prefixAt, prefixAt + 3)
      const found = src.indexOf(quote, prefixAt + 3)
      const end = found < 0 ? src.length : found + 3
      push(tokens, "string", src.slice(i, end))
      i = end
      pendingName = false
      continue
    }
    const q = src[prefixAt]
    if (q === '"' || q === "'") {
      const end = readQuoted(src, prefixAt)
      push(tokens, "string", src.slice(i, end))
      i = end
      pendingName = false
      continue
    }
    if (def.backticks && c === "`") {
      i = def.backticks === "template" ? readTemplate(src, i, tokens) : readRawTick(src, i, tokens)
      pendingName = false
      continue
    }
    const num = readNumber(src, i)
    if (num > i) {
      push(tokens, "flag", src.slice(i, num))
      i = num
      pendingName = false
      continue
    }
    const op = matchOp(src, i)
    if (op) {
      push(tokens, "operator", op)
      i += op.length
      if (op !== ".") pendingName = false
      continue
    }
    if (isIdentStart(c)) {
      const start = i
      i++
      while (i < src.length && isIdentPart(src[i])) i++
      const word = src.slice(start, i)
      const folded = def.caseInsensitive ? word.toLowerCase() : word
      if (isKeyword(def, folded)) {
        push(tokens, "keyword", word)
        pendingName = def.nameLeaders.has(folded) || def.nameLeaders.has(word)
        continue
      }
      if (pendingName) {
        push(tokens, "command", word)
        pendingName = false
        continue
      }
      if (def.keyOp && nextNonSpaceIs(src, i, def.keyOp)) {
        push(tokens, "variable", word)
        continue
      }
      push(tokens, "text", word)
      continue
    }
    push(tokens, "operator", c)
    i++
    pendingName = false
  }
  return tokens
}

function tokenizeMarkup(src: string): SourceToken[] {
  const tokens: SourceToken[] = []
  let i = 0
  while (i < src.length) {
    if (src.startsWith("<!--", i)) {
      const found = src.indexOf("-->", i + 4)
      const end = found < 0 ? src.length : found + 3
      push(tokens, "comment", src.slice(i, end))
      i = end
      continue
    }
    if (src[i] === "<") {
      push(tokens, "operator", "<")
      i++
      if (src[i] === "/" || src[i] === "!" || src[i] === "?") {
        push(tokens, "operator", src[i])
        i++
      }
      const nameStart = i
      while (i < src.length && isTagNameChar(src[i])) i++
      if (i > nameStart) push(tokens, "command", src.slice(nameStart, i))
      while (i < src.length && src[i] !== ">") {
        if (src[i] === "/" && src[i + 1] === ">") break
        if (isSpace(src[i])) {
          const start = i
          while (i < src.length && isSpace(src[i])) i++
          push(tokens, "text", src.slice(start, i))
          continue
        }
        if (src[i] === '"' || src[i] === "'") {
          const end = readQuoted(src, i)
          push(tokens, "string", src.slice(i, end))
          i = end
          continue
        }
        if (src[i] === "=") {
          push(tokens, "operator", "=")
          i++
          continue
        }
        const attr = i
        while (i < src.length && isAttrNameChar(src[i])) i++
        if (i > attr) {
          push(tokens, "variable", src.slice(attr, i))
          continue
        }
        push(tokens, "text", src[i])
        i++
      }
      if (src[i] === "/") {
        push(tokens, "operator", "/")
        i++
      }
      if (src[i] === ">") {
        push(tokens, "operator", ">")
        i++
      }
      continue
    }
    const start = i
    while (i < src.length && src[i] !== "<") i++
    push(tokens, "text", src.slice(start, i))
  }
  return tokens
}

function readTemplate(src: string, i: number, tokens: SourceToken[]): number {
  let start = i
  i++
  while (i < src.length) {
    if (src[i] === "\\") {
      i += 2
      continue
    }
    if (src.startsWith("${", i)) {
      if (i > start) push(tokens, "string", src.slice(start, i))
      push(tokens, "operator", "${")
      i += 2
      const close = matchingBrace(src, i)
      const inner = tokenizeSource(src.slice(i, close), "js")
      for (const t of inner) push(tokens, t.kind, t.text)
      if (close < src.length && src[close] === "}") {
        push(tokens, "operator", "}")
        i = close + 1
      } else {
        i = close
      }
      start = i
      continue
    }
    if (src[i] === "`") {
      push(tokens, "string", src.slice(start, i + 1))
      return i + 1
    }
    i++
  }
  if (i > start) push(tokens, "string", src.slice(start, i))
  return src.length
}

function readRawTick(src: string, i: number, tokens: SourceToken[]): number {
  const start = i
  i++
  while (i < src.length && src[i] !== "`") i++
  if (i < src.length) i++
  push(tokens, "string", src.slice(start, i))
  return i
}

function readQuoted(src: string, i: number): number {
  const q = src[i]
  i++
  while (i < src.length) {
    if (src[i] === "\\") {
      i += 2
      continue
    }
    if (src[i] === q) return i + 1
    i++
  }
  return src.length
}

function stringPrefixEnd(src: string, i: number): number {
  let j = i
  while (j < src.length && /[frbuFRBU]/.test(src[j])) j++
  if (j === i) return i
  if (src[j] === '"' || src[j] === "'") return j
  return i
}

function isTriple(src: string, i: number): boolean {
  return src.startsWith('"""', i) || src.startsWith("'''", i)
}

function readNumber(src: string, i: number): number {
  if (src[i] === "." && isDigit(src[i + 1])) return readFrac(src, i)
  if (!isDigit(src[i])) return i
  if (src[i] === "0" && src[i + 1] && "xXbBoO".includes(src[i + 1])) {
    let j = i + 2
    while (j < src.length && /[0-9A-Fa-f_]/.test(src[j])) j++
    return j === i + 2 ? i + 1 : j
  }
  let j = i
  while (j < src.length && /[0-9_]/.test(src[j])) j++
  if (src[j] === "." && isDigit(src[j + 1])) j = readFrac(src, j)
  else if (src[j] === "e" || src[j] === "E") j = readExp(src, j)
  return j
}

function readFrac(src: string, i: number): number {
  let j = i + 1
  while (j < src.length && /[0-9_]/.test(src[j])) j++
  if (src[j] === "e" || src[j] === "E") return readExp(src, j)
  return j
}

function readExp(src: string, i: number): number {
  let j = i + 1
  if (src[j] === "+" || src[j] === "-") j++
  if (!isDigit(src[j])) return i
  while (j < src.length && /[0-9_]/.test(src[j])) j++
  return j
}

function readDollarIdent(src: string, i: number): number {
  let j = i + 1
  if (src[j] === "{") {
    let depth = 1
    j++
    while (j < src.length && depth > 0) {
      if (src[j] === "{") depth++
      else if (src[j] === "}") depth--
      j++
    }
    return j
  }
  while (j < src.length && isIdentPart(src[j])) j++
  return j === i + 1 ? i + 1 : j
}

function looksLikeLifetime(src: string, i: number): boolean {
  return src[i] === "'" && isIdentStart(src[i + 1] ?? "") && src[i + 2] !== "'"
}

function readLifetime(src: string, i: number): number {
  let j = i + 1
  while (j < src.length && isIdentPart(src[j])) j++
  return j
}

function matchingBrace(src: string, i: number): number {
  let depth = 1
  while (i < src.length && depth > 0) {
    if (src[i] === '"' || src[i] === "'" || src[i] === "`") {
      if (src[i] === "`") {
        i++
        while (i < src.length && src[i] !== "`") {
          if (src[i] === "\\") i += 2
          else i++
        }
        if (i < src.length) i++
        continue
      }
      i = readQuoted(src, i)
      continue
    }
    if (src[i] === "{") depth++
    else if (src[i] === "}") {
      depth--
      if (depth === 0) return i
    }
    i++
  }
  return src.length
}

function matchOp(src: string, i: number): string | undefined {
  for (const op of OPS) {
    if (src.startsWith(op, i)) return op
  }
  const c = src[i]
  if (c && "{}[](),;.:?~^|&*/%+-!=<>@#".includes(c)) return c
  return undefined
}

function nextNonSpaceIs(src: string, i: number, op: string): boolean {
  let j = i
  while (j < src.length && (src[j] === " " || src[j] === "\t")) j++
  if (op === ":") return src[j] === ":" && src[j + 1] !== ":"
  return src.startsWith(op, j)
}

function isKeyword(def: LangDef, folded: string): boolean {
  if (def.caseInsensitive) {
    for (const k of def.keywords) {
      if (k.toLowerCase() === folded) return true
    }
    return false
  }
  return def.keywords.has(folded)
}

function push(tokens: SourceToken[], kind: SourceTokenKind, text: string) {
  if (!text) return
  const last = tokens[tokens.length - 1]
  if (last && last.kind === kind) {
    last.text += text
    return
  }
  tokens.push({ kind, text })
}

function isSpace(c: string | undefined): boolean {
  return c === " " || c === "\t" || c === "\r" || c === "\n"
}

function isDigit(c: string | undefined): boolean {
  return Boolean(c && c >= "0" && c <= "9")
}

function isHex(c: string | undefined): boolean {
  return Boolean(c && /[0-9A-Fa-f]/.test(c))
}

function isIdentStart(c: string): boolean {
  return /[A-Za-z_\u00C0-\uFFFF]/.test(c)
}

function isIdentPart(c: string): boolean {
  return /[A-Za-z0-9_\u00C0-\uFFFF]/.test(c)
}

function isTagNameChar(c: string): boolean {
  return /[A-Za-z0-9:-]/.test(c)
}

function isAttrNameChar(c: string): boolean {
  return /[A-Za-z0-9:_-]/.test(c)
}
