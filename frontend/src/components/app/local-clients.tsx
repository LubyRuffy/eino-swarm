import { useEffect, useState } from "react"

import { SidebarSection } from "@/components/app/sidebar-section"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { openClient, useOpenClient } from "@/lib/client-open"
import { chromeTypeClass } from "@/lib/chrome-type"
import {
  mergeClientTools,
  toolLabelKey,
  type ClientTool,
} from "@/lib/local-clients"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

const POLL_MS = 1500

/** Read-only tool groups. A row opens the session in the main chat.
 *  The composer stays on screen and cannot send. */
export function LocalClientsSection() {
  const t = useT()
  const [enabled, setEnabled] = useState(false)
  const [tools, setTools] = useState<ClientTool[]>([])
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const reading = useOpenClient()

  useEffect(() => {
    let stop = false
    const tick = () => {
      api
        .clients()
        .then((cat) => {
          if (stop) return
          setEnabled((on) => (on === cat.enabled ? on : cat.enabled))
          setTools((prev) => {
            const incoming = cat.tools ?? []
            if (!cat.enabled && prev.length === 0 && incoming.length === 0) return prev
            return mergeClientTools(prev, incoming, "replace")
          })
        })
        .catch(() => undefined)
    }
    tick()
    const id = setInterval(tick, POLL_MS)
    return () => {
      stop = true
      clearInterval(id)
    }
  }, [])

  if (!enabled) return null

  const more = (tool: ClientTool) => {
    if (!tool.next) return
    api
      .clients(tool.next)
      .then((cat) => {
        const one = (cat.tools ?? []).filter((item) => item.id === tool.id)
        setTools((prev) => mergeClientTools(prev, one, "append"))
      })
      .catch(() => undefined)
  }

  return (
    <SidebarSection
      testId="clients-list"
      label={t("sidebar.clients")}
      open={open.clients !== false}
      onToggle={() => setOpen((cur) => ({ ...cur, clients: cur.clients === false }))}
    >
      {tools.map((tool) => (
        <SidebarSection
          key={tool.id}
          testId={`client-tool-${tool.id}`}
          label={t(toolLabelKey(tool.id))}
          open={open[tool.id] !== false}
          onToggle={() => setOpen((cur) => ({ ...cur, [tool.id]: cur[tool.id] === false }))}
        >
          {tool.tasks.length === 0 ? (
            <p className={cn(chromeTypeClass, "px-[var(--sidebar-row-px)] pb-1 text-sidebar-foreground/60")}>
              {t("sidebar.clientEmpty")}
            </p>
          ) : (
            <ul>
              {tool.tasks.map((task) => (
                <li key={task.id} data-testid="client-task">
                  <button
                    type="button"
                    className={cn(
                      "flex w-full items-center gap-2 px-[var(--sidebar-row-px)] py-1 text-left",
                      reading?.id === task.id && "bg-sidebar-accent text-sidebar-accent-foreground",
                    )}
                    onClick={() => openClient({ id: task.id, title: task.title, status: task.status })}
                  >
                  <span
                    data-testid="client-status"
                    data-status={task.status}
                    role="img"
                    aria-label={
                      task.status === "running" ? t("sidebar.clientRunning") : t("sidebar.clientDone")
                    }
                    className={cn(
                      "size-2 shrink-0 rounded-full",
                      task.status === "running"
                        ? "bg-primary motion-safe:animate-pulse motion-reduce:animate-none"
                        : "bg-muted-foreground",
                    )}
                  />
                  <span className={cn(chromeTypeClass, "min-w-0 flex-1 truncate text-sidebar-foreground")}>
                    {task.title}
                  </span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          {tool.more ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="mx-1"
              data-testid={`client-more-${tool.id}`}
              onClick={() => more(tool)}
            >
              {t("sidebar.clientMore")}
            </Button>
          ) : null}
        </SidebarSection>
      ))}
    </SidebarSection>
  )
}
