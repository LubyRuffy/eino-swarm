import { useState } from "react"
import { ChevronLeft, ChevronRight } from "lucide-react"

import { Composer } from "@/components/composer"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"
import { clientToolTitle } from "@/lib/local-clients"
import type { ClientTool, ClientView } from "@/lib/rpc"

type Reading = {
  title: string
  status: string
  entries: { role: string; text: string }[]
  truncated: boolean
  failed: boolean
  loading: boolean
}

/** A row opens the same chat column as a PC thread. The composer is on
 *  screen and cannot send. */
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
  const [view, setView] = useState<Reading | null>(null)
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
                      const shell: Reading = {
                        title: task.title,
                        status: task.status,
                        entries: [],
                        truncated: false,
                        failed: false,
                        loading: true,
                      }
                      setView(shell)
                      void onRead?.(task.id).then((doc) => {
                        if (!doc) {
                          setView({ ...shell, loading: false, failed: true })
                          return
                        }
                        setView({
                          title: doc.title || task.title,
                          status: doc.status || task.status,
                          entries: doc.entries ?? [],
                          truncated: Boolean(doc.truncated),
                          failed: false,
                          loading: false,
                        })
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
        <section className="fixed inset-0 z-40 flex flex-col bg-background" data-testid="client-transcript">
          <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border px-2">
            <Button
              type="button"
              variant="ghost"
              className="size-9 px-0"
              aria-label={t("thread.back")}
              onClick={() => setView(null)}
            >
              <ChevronLeft className="size-5" />
            </Button>
            <span
              className={cn(
                "size-1.5 shrink-0 rounded-full",
                view.status === "running"
                  ? "bg-primary motion-safe:animate-pulse motion-reduce:animate-none"
                  : "bg-muted-foreground",
              )}
            />
            <h1 className="min-w-0 flex-1 truncate text-sm font-medium">{view.title}</h1>
          </header>
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-4">
            {view.loading ? <p className="text-sm text-muted-foreground">{t("thread.loading")}</p> : null}
            {view.failed ? <p className="text-sm text-muted-foreground">{t("home.clientMissing")}</p> : null}
            {(view.entries ?? []).map((entry, i) =>
              entry.role === "user" ? (
                <div key={`${entry.role}-${i}`} className="flex justify-end">
                  <p className="max-w-[85%] rounded-2xl rounded-br-md bg-secondary px-3 py-2 text-sm text-secondary-foreground">
                    {entry.text}
                  </p>
                </div>
              ) : (
                <p
                  key={`${entry.role}-${i}`}
                  className={cn("text-sm", entry.role === "tool" && "text-xs text-muted-foreground")}
                >
                  {entry.text}
                </p>
              ),
            )}
            {view.truncated ? (
              <p className="text-xs text-muted-foreground">{t("home.clientTruncated")}</p>
            ) : null}
          </div>
          <Composer
            label={t("thread.message")}
            sendLabel={t("thread.send")}
            placeholder={t("home.clientReadOnly")}
            disabled
            onSubmit={() => undefined}
          />
        </section>
      ) : null}
    </section>
  )
}
