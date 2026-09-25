import { Button } from "@/components/ui/button"
import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { clientToolTitle } from "@/lib/local-clients"
import type { ClientTool } from "@/lib/rpc"

/** Read-only. A task row is not a control: the phone cannot steer it. */
export function ClientGroups({
  tools,
  onMore,
}: {
  tools: ClientTool[]
  onMore: (id: string, next?: string) => void
}) {
  if (tools.length === 0) return null
  return (
    <section className="flex flex-col gap-2" data-testid="client-groups">
      <h2 className="px-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        {t("home.clients")}
      </h2>
      {tools.map((tool) => (
        <div key={tool.id} data-testid={`client-tool-${tool.id}`} className="flex flex-col gap-1">
          <h3 className="px-1 text-[11px] font-medium text-muted-foreground">{t(clientToolTitle(tool.id))}</h3>
          <ul className="overflow-hidden rounded-2xl border border-border bg-card">
            {tool.tasks.length === 0 ? (
              <li className="px-3 py-2 text-xs text-muted-foreground">{t("home.clientEmpty")}</li>
            ) : (
              tool.tasks.map((task) => (
                <li
                  key={task.id}
                  data-testid="client-task"
                  className="flex items-center gap-2 border-b border-border px-3 py-2 last:border-b-0"
                >
                  <span
                    data-testid="client-status"
                    data-status={task.status}
                    role="img"
                    aria-label={task.status === "running" ? t("home.clientRunning") : t("home.clientDone")}
                    className={cn(
                      "size-2 shrink-0 rounded-full",
                      task.status === "running"
                        ? "bg-primary motion-safe:animate-pulse motion-reduce:animate-none"
                        : "bg-muted-foreground",
                    )}
                  />
                  <span className="min-w-0 flex-1 truncate text-sm">{task.title}</span>
                </li>
              ))
            )}
            {tool.more ? (
              <li>
                <Button
                  variant="ghost"
                  className="h-9 w-full rounded-none text-muted-foreground"
                  data-testid={`client-more-${tool.id}`}
                  onClick={() => onMore(tool.id, tool.next)}
                >
                  {t("home.more")}
                </Button>
              </li>
            ) : null}
          </ul>
        </div>
      ))}
    </section>
  )
}
