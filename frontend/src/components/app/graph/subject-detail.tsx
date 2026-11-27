import { X } from "lucide-react"
import { attrLabel, kindName } from "../../../lib/graph/kinds"
import type { Ring } from "../../../lib/graph/analysis"
import type { GraphSubject } from "../../../lib/graph/types"
import type { GraphTranslate } from "../../../lib/messages-graph"
import { kindCssColor } from "./kind-style"

function show(value: unknown): string {
  if (Array.isArray(value)) return value.map(show).join(", ")
  if (value !== null && typeof value === "object") return JSON.stringify(value)
  return String(value)
}

const SECRET_MASK = "••••••"

/** Attributes stay readable here because the model's own digest cannot show
 *  them; a credential is only ever a mask, never a value. */
export function SubjectDetail({ subject, ring, t, onClose }: {
  subject: GraphSubject
  ring: Ring
  t: GraphTranslate
  onClose: () => void
}) {
  const attrs = Object.entries(subject.attrs ?? {}).filter(([, value]) => value !== "" && value !== null && value !== undefined)
  const secrets = subject.credentials ?? []
  return (
    <section aria-label={subject.name} className="rounded-xl border bg-card p-3 text-xs">
      <header className="flex items-start gap-2">
        <span aria-hidden className="mt-1 size-2.5 shrink-0 rounded-full" style={{ background: kindCssColor(subject.kind) }} />
        <div className="min-w-0 flex-1">
          <p className="m-0 break-words text-sm font-medium">{subject.name}</p>
          <p className="m-0 text-muted-foreground">
            {kindName(subject.kind, t.locale)} · {t(`graph.status.${subject.status}`)} · {t(`graph.ring.${ring}`)}
            {subject.evidence?.length ? ` · ${t("graph.evidence", { n: subject.evidence.length })}` : ""}
          </p>
        </div>
        <button type="button" aria-label={t("graph.closeDetail")} onClick={onClose}
          className="grid size-6 place-items-center rounded-md text-muted-foreground hover:text-foreground">
          <X className="size-4" aria-hidden />
        </button>
      </header>
      {attrs.length || secrets.length ? (
        <dl className="m-0 mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
          {attrs.map(([name, value]) => (
            <div key={name} className="contents">
              <dt className="text-muted-foreground">{attrLabel(subject.kind, name, t.locale)}</dt>
              <dd className="m-0 min-w-0 break-words">{show(value)}</dd>
            </div>
          ))}
          {secrets.map((name) => (
            <div key={`secret:${name}`} className="contents">
              <dt className="text-muted-foreground">{attrLabel(subject.kind, name, t.locale)}</dt>
              <dd className="m-0" aria-label={t("graph.secretValue")}>{SECRET_MASK}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </section>
  )
}
