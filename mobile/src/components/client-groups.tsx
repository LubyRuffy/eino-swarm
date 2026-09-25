import { useState } from "react"
import { ChevronRight } from "lucide-react"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { clientToolTitle } from "@/lib/local-clients"
import type { ClientTool, ClientView } from "@/lib/rpc"

/** Read-only. A task row is not a control: the phone cannot steer it. */
export function ClientGroups({
  tools,
  onMore,
  onRead,
}: {
  tools: ClientTool[]
  onMore: (id: string, next?: string) => void
  onRead?: (id: string) => Promise<ClientView | null>
}) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [view, setView] = useState<ClientView | null>(null)
  if (tools.length === 0) return null
  return (
    <section className="flex flex-col gap-2" data-testid="client-groups">
      <h2 className="px-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">
        {t("home.clients")}
      </h2>
      {tools.map((tool) => (
        <div key={tool.id} data-testid={`client-tool-${tool.id}`} className="flex flex-col gap-1">
          <h3 className="px-1 text-[11px] font-medium text-muted-foreground">
            <button
              type="button"
              aria-expanded={open[tool.id] !== false}
              className="flex w-full items-center gap-1 text-left"
              onClick={() => setOpen((cur) => ({ ...cur, [tool.id]: cur[tool.id] === false }))}
            >
              <ChevronRight
                className={cn(
                  "size-3 shrink-0",
                  open[tool.id] !== false && "rotate-90",
                )}
                aria-hidden
              />
              {t(clientToolTitle(tool.id))}
            </button>
          </h3>
          {open[tool.id] === false ? null : (
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
                  <button
                    type="button"
                    className="min-w-0 flex-1 truncate text-left text-sm"
                    onClick={() => {
                      void onRead?.(task.id).then((doc) => {
                        if (doc) setView(doc)
                      })
                    }}
                  >
                    {task.title}
                  </button>
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
          )}
        </div>
      ))}
      {view ? (
        <section className="fixed inset-0 z-40 flex flex-col gap-3 bg-background p-4" data-testid="client-transcript">
          <button type="button" className="text-left text-sm text-muted-foreground" onClick={() => setView(null)}>
            {t("thread.back")}
          </button>
          <h2 className="text-base font-medium">{view.title}</h2>
          <p className="text-xs text-muted-foreground">{t("home.clientReadOnly")}</p>
          <div className="min-h-0 flex-1 space-y-2 overflow-y-auto">
            {view.entries.map((entry, i) => (
              <p key={`${entry.role}-${i}`} className="text-sm">
                <span className="text-muted-foreground">{entry.role}</span> {entry.text}
              </p>
            ))}
            {view.truncated ? <p className="text-xs text-muted-foreground">{t("home.clientTruncated")}</p> : null}
          </div>
        </section>
      ) : null}
    </section>
  )
}
