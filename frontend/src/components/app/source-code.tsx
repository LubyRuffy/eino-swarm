import { useMemo } from "react"

import { paintLines, type SourceTokenKind } from "@/lib/source-highlight"
import type { ReadLine } from "@/lib/read-result"

const KIND_CLASS: Record<SourceTokenKind, string> = {
  text: "",
  comment: "text-syntax-comment",
  string: "text-syntax-string",
  keyword: "text-syntax-keyword",
  command: "text-syntax-command",
  operator: "text-syntax-operator",
  flag: "text-syntax-flag",
  variable: "text-syntax-variable",
}

/** Numbered file body. Tokens come from the path's suffix; an unknown
 *  suffix stays one uncoloured span so Find still sees the original text. */
export function SourceListing({
  path,
  lines,
  body,
}: {
  path: string
  lines: ReadLine[]
  body: string
}) {
  const rows = useMemo(() => paintLines(lines, body, path), [lines, body, path])
  return (
    <div className="thin-scrollbar max-h-72 overflow-auto">
      <table className="w-full font-mono">
        <tbody>
          {rows.map((row) => (
            <tr key={row.n} className="align-top">
              <td className="select-none whitespace-nowrap px-2 py-0 text-right text-muted-foreground">
                {row.n}
              </td>
              <td className="stream-text w-full whitespace-pre-wrap py-0 pr-2">
                {row.tokens.map((t, i) => (
                  <span key={i} className={KIND_CLASS[t.kind] || undefined}>
                    {t.text}
                  </span>
                ))}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
