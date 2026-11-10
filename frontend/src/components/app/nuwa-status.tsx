import { Badge } from "@/components/ui/badge"
import { AlertTriangle } from "lucide-react"
import type { NuwaNode } from "@/lib/nuwa"
import { useT, type Translate } from "@/lib/use-t"

export function nuwaKind(kind: string, t: Translate): string {
  if (kind === "task") return t("nuwa.task")
  if (kind === "fact") return t("nuwa.fact")
  if (kind === "artifact") return t("nuwa.artifact")
  return kind
}

export function NuwaStale({ node }: { node: NuwaNode }) {
  const t = useT()
  if (!node.stale_dependencies?.length) return null
  return (
    <span
      className="inline-flex items-center gap-1 text-[10px] text-running"
      title={t("nuwa.stale")}
    >
      <AlertTriangle className="size-3 shrink-0" />
      {t("nuwa.staleShort")}
    </span>
  )
}

export function nuwaStatusLabel(status: string, t: Translate) {
  const labels: Record<string, string> = {
    pending: t("nuwa.pending"),
    running: t("nuwa.running"),
    done: t("nuwa.done"),
    blocked: t("nuwa.blocked"),
    failed: t("nuwa.failed"),
    cancelled: t("nuwa.cancelled"),
    reported: t("nuwa.reported"),
    superseded: t("nuwa.superseded"),
    retracted: t("nuwa.retracted"),
  }
  return labels[status] ?? status
}

export function NuwaStatus({ status }: { status: string }) {
  const t = useT()
  const variant =
    status === "done"
      ? "success"
      : status === "running"
        ? "warning"
        : status === "blocked" || status === "failed"
          ? "danger"
          : "outline"
  return (
    <Badge variant={variant} className="max-w-full truncate px-1.5 py-0 text-[10px]">
      {nuwaStatusLabel(status, t)}
    </Badge>
  )
}
