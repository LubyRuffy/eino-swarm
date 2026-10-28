import { useId, useState } from "react"

import { EditDiffView } from "@/components/app/edit-diff"
import { Button } from "@/components/ui/button"
import { DIFF_LINE_CAP, diffText } from "@/lib/edit-diff"
import type { MemoryChange } from "@/lib/types"
import { useT, type Translate } from "@/lib/use-t"

function changeAction(change: MemoryChange, t: Translate): string {
  switch (change.action) {
    case "add": return t("memory.changeAdded")
    case "create": return t(change.before ? "memory.changeUpdated" : "memory.changeAdded")
    case "remove": case "delete": return t("memory.changeDeleted")
    case "merge": return t("memory.changeMerged")
    case "promote": return t("memory.changePromoted")
    case "demote": return t("memory.changeDemoted")
    default: return t("memory.changeUpdated")
  }
}

/** Old previews cannot establish what was removed, and current memory cannot
 *  reconstruct a historical write. Only paired committed images form a diff. */
export function ReviewChanges({ changes }: { changes?: MemoryChange[] }) {
  const t = useT()
  if (!changes?.length) return <p>{t("memory.changeUnavailable")}</p>
  return (
    <div className="space-y-3">
      {changes.map((change, index) => {
        const label = change.name ? t("memory.changeSkill", { name: change.name }) : t("memory.changeNote")
        const complete = typeof change.before === "string" && typeof change.after === "string"
        return (
          <div key={index} data-testid="memory-change">
            <p className="mb-1 text-xs font-medium text-foreground">{label} · {changeAction(change, t)}</p>
            {complete ? (
              <CommittedChange label={label} before={change.before!} after={change.after!} />
            ) : (
              <div className="space-y-1">
                <p className="text-xs text-muted-foreground">{t("memory.changePreviewOnly")}</p>
                {change.text ? <pre className="stream-text whitespace-pre-wrap break-words text-foreground">{change.text}</pre> : null}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

function CommittedChange({ label, before, after }: { label: string; before: string; after: string }) {
  const t = useT()
  const [fullTextOpen, setFullTextOpen] = useState(false)
  const fullTextId = useId()
  const diff = diffText(label, before, after)
  const clipped = diff.hunks.reduce((total, hunk) => total + hunk.lines.length, 0) > DIFF_LINE_CAP
  return (
    <>
      <EditDiffView diff={diff} />
      {clipped ? (
        <Button type="button" variant="link" size="sm" className="h-auto px-0 text-xs" aria-expanded={fullTextOpen} aria-controls={fullTextId} onClick={() => setFullTextOpen((open) => !open)}>
          {t(fullTextOpen ? "memory.changeFullTextHide" : "memory.changeFullText")}
        </Button>
      ) : null}
      {clipped && fullTextOpen ? (
        <div id={fullTextId} role="region" aria-label={t("memory.changeFullText")} className="space-y-2">
          {[[t("memory.changeBefore"), before], [t("memory.changeAfter"), after]].map(([title, text]) => (
            <div key={title}>
              <p className="text-xs font-medium text-foreground">{title}</p>
              <pre className="stream-text thin-scrollbar max-h-72 overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-2 text-foreground">{text || t("memory.changeEmpty")}</pre>
            </div>
          ))}
        </div>
      ) : null}
    </>
  )
}
