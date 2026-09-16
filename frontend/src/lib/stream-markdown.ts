/** Close the inline markers a streaming markdown parser would otherwise
 *  leave as punctuation until the matching token arrives.
 *
 *  Headings, lists and unclosed fences are already valid as they grow —
 *  CommonMark treats an open fence as a code block through the end of the
 *  document, which is the right live view. This only patches `**`, `***`,
 *  `~~` and `` ` `` in the current prose region, which otherwise flash as
 *  raw characters and then snap to formatting.
 *
 *  Complete answers must not go through here: a closer is a live lie that
 *  the finishing token retracts. */
export function closeIncompleteMarkdown(text: string): string {
  if (!text) return text
  const region = lastProseRegion(text)
  if (!region) return text
  const open = openInlines(region.text)
  if (open.length === 0) return text
  return text + open.reverse().join("")
}

const FENCE = /^( {0,3})(`{3,}|~{3,})(.*)$/

/** The prose after the last closed fence, or null when a fence is still
 *  open — in which case the rest of the document is already a code block. */
function lastProseRegion(text: string): { text: string } | null {
  let fence: { ch: string; len: number } | null = null
  let proseStart = 0
  let start = 0
  while (start <= text.length) {
    const nl = text.indexOf("\n", start)
    const end = nl === -1 ? text.length : nl
    const line = text.slice(start, end)
    const m = FENCE.exec(line)
    if (m) {
      const marker = m[2]
      const ch = marker[0]
      const info = m[3]
      if (!fence) {
        if (!(ch === "`" && info.includes("`"))) {
          fence = { ch, len: marker.length }
        }
      } else if (ch === fence.ch && marker.length >= fence.len && info.trim() === "") {
        fence = null
        proseStart = end + (nl === -1 ? 0 : 1)
      }
    }
    if (nl === -1) break
    start = nl + 1
  }
  if (fence) return null
  return { text: text.slice(proseStart) }
}

/** Markers still open at the end of `s`, innermost last. A marker with no
 *  content after it is left alone: closing `**` to `****` is worse than
 *  showing the opener for a frame. */
function openInlines(s: string): string[] {
  const stack: string[] = []
  let inCode = false
  for (let i = 0; i < s.length; ) {
    if (s[i] === "\\" && i + 1 < s.length) {
      i += 2
      continue
    }
    if (s[i] === "`") {
      inCode = !inCode
      toggle(stack, "`")
      i++
      continue
    }
    if (inCode) {
      i++
      continue
    }
    if (s.startsWith("~~", i)) {
      toggle(stack, "~~")
      i += 2
      continue
    }
    if (s.startsWith("***", i)) {
      toggle(stack, "***")
      i += 3
      continue
    }
    if (s.startsWith("**", i)) {
      toggle(stack, "**")
      i += 2
      continue
    }
    i++
  }
  return stack.filter((tok) => hasContentAfter(s, tok))
}

function toggle(stack: string[], token: string) {
  if (stack.at(-1) === token) stack.pop()
  else stack.push(token)
}

function hasContentAfter(s: string, tok: string): boolean {
  const i = s.lastIndexOf(tok)
  if (i === -1) return false
  return s.slice(i + tok.length).trim().length > 0
}
