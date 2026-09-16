import { useMemo } from "react"

import { tokenizeShell, type ShellTokenKind } from "@/lib/shell-highlight"
import { cn } from "@/lib/utils"

const KIND_CLASS: Record<ShellTokenKind, string> = {
  text: "",
  comment: "text-syntax-comment",
  string: "text-syntax-string",
  keyword: "text-syntax-keyword",
  command: "text-syntax-command",
  operator: "text-syntax-operator",
  flag: "text-syntax-flag",
  variable: "text-syntax-variable",
}

/** An exec invocation. Collapsed: one truncated highlighted line. Expanded:
 *  the whole command wraps, Codex-style, with a prompt so it reads as a
 *  shell. */
export function ShellCommand({
  command,
  compact = false,
}: {
  command: string
  compact?: boolean
}) {
  const source = compact ? command.replace(/\s+/g, " ").trim() : command
  const tokens = useMemo(() => tokenizeShell(source), [source])
  const Tag = compact ? "span" : "pre"
  return (
    <Tag
      data-testid={compact ? "shell-command-preview" : "shell-command"}
      data-find-ignore={compact ? "" : undefined}
      className={cn(
        "font-mono",
        compact
          ? "block truncate text-[13px]"
          : "thin-scrollbar max-h-72 overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-2 text-[12px]",
      )}
    >
      {compact ? null : (
        <span aria-hidden className="select-none text-syntax-prompt">
          ${" "}
        </span>
      )}
      {tokens.map((t, i) => (
        <span key={i} className={KIND_CLASS[t.kind] || undefined}>
          {t.text}
        </span>
      ))}
    </Tag>
  )
}
