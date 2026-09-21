import { useMemo } from "react"

import { SOURCE_KIND_CLASS } from "@/components/app/source-code"
import {
  clipDiff,
  editCountLabel,
  type DiffOp,
  type EditDiff,
} from "@/lib/edit-diff"
import { languageFromPath, tokenizeSource, type SourceToken } from "@/lib/source-highlight"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"

const ROW_CLASS: Record<DiffOp, string> = {
  eq: "",
  add: "bg-diff-add-bg",
  del: "bg-diff-del-bg",
}

const MARK_CLASS: Record<DiffOp, string> = {
  eq: "text-muted-foreground",
  add: "text-diff-add",
  del: "text-diff-del",
}

const MARK: Record<DiffOp, string> = {
  eq: " ",
  add: "+",
  del: "−",
}

/** Collapsed `+N −M`. Idle inherits the row colour; hover/focus paints
 *  add/del so the counts stay quiet until you are looking at them.
 *  Needs a `group` ancestor — Disclosure already has one. */
export function EditCountMarks({
  added,
  removed,
  className,
}: {
  added: number
  removed: number
  className?: string
}) {
  if (added <= 0 && removed <= 0) return null
  return (
    <span
      data-testid="edit-counts"
      className={cn("inline-flex shrink-0 items-baseline gap-1 tabular-nums", className)}
    >
      {added > 0 ? (
        <span className="transition-colors group-hover:text-diff-add group-focus-within:text-diff-add">
          +{added}
        </span>
      ) : null}
      {removed > 0 ? (
        <span className="transition-colors group-hover:text-diff-del group-focus-within:text-diff-del">
          −{removed}
        </span>
      ) : null}
    </span>
  )
}

/** Highlighted hunk for an `edit` / `write` call. Built from the args, not
 *  the status sentence the tool returns to the model. */
export function EditDiffView({ diff }: { diff: EditDiff }) {
  const t = useT()
  const painted = useMemo(() => paintDiff(diff), [diff])
  const counts = diff.added || diff.removed ? editCountLabel(diff) : ""
  const label = [diff.path, counts].filter(Boolean).join(" · ")
  return (
    <div
      className="overflow-hidden rounded bg-muted"
      data-testid="file-diff"
      role="region"
      aria-label={label}
    >
      {label ? (
        <p className="flex min-w-0 items-center gap-1.5 border-b border-border px-2 py-1 text-[11px] text-muted-foreground">
          {diff.path ? <span className="min-w-0 truncate">{diff.path}</span> : null}
          <EditCountMarks added={diff.added} removed={diff.removed} />
        </p>
      ) : null}
      {painted.empty ? (
        <p className="px-2 py-2 text-muted-foreground">{t("tool.emptyFile")}</p>
      ) : (
        <div className="thin-scrollbar max-h-72 overflow-auto">
          {painted.hunks.map((hunk, hi) => (
            <div key={hi}>
              {hunk.header ? (
                <p className="bg-accent/40 px-2 py-0.5 font-mono text-[11px] text-muted-foreground">
                  {hunk.header}
                </p>
              ) : null}
              <table className="w-full font-mono">
                <tbody>
                  {hunk.rows.map((row, i) => (
                    <DiffRow key={i} op={row.op} tokens={row.tokens} />
                  ))}
                </tbody>
              </table>
            </div>
          ))}
          {painted.hidden > 0 ? (
            <p className="px-2 py-1 text-[11px] text-muted-foreground" data-testid="file-diff-more">
              {t("tool.diffMore", { n: painted.hidden })}
            </p>
          ) : null}
        </div>
      )}
    </div>
  )
}

function DiffRow({ op, tokens }: { op: DiffOp; tokens: SourceToken[] }) {
  return (
    <tr data-diff={op} className={`align-top ${ROW_CLASS[op]}`}>
      <td
        className={`w-4 select-none whitespace-nowrap px-1.5 py-0 text-center ${MARK_CLASS[op]}`}
      >
        {MARK[op]}
      </td>
      <td className="stream-text w-full whitespace-pre-wrap py-0 pr-2">
        {tokens.length === 0 ? "\u00a0" : tokens.map((tok, i) => (
          <span key={i} className={SOURCE_KIND_CLASS[tok.kind] || undefined}>
            {tok.text}
          </span>
        ))}
      </td>
    </tr>
  )
}

function paintDiff(diff: EditDiff) {
  const lang = languageFromPath(diff.path)
  const clipped = clipDiff(diff)
  const empty = clipped.hunks.every((h) => h.lines.length === 0)
  return {
    empty,
    hidden: clipped.hidden,
    hunks: clipped.hunks.map((hunk) => ({
      header: hunk.header,
      rows: hunk.lines.map((line) => ({
        op: line.op,
        tokens: paintDiffLine(line.text, lang),
      })),
    })),
  }
}

function paintDiffLine(text: string, lang: string | undefined): SourceToken[] {
  if (!text) return []
  if (!lang) return [{ kind: "text", text }]
  const tokens = tokenizeSource(text, lang)
  return tokens.length ? tokens : [{ kind: "text", text }]
}
