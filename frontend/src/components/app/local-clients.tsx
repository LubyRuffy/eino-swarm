import { useEffect, useState, useSyncExternalStore } from "react"

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

type CatalogSnap = {
  enabled: boolean
  tools: ClientTool[]
  settled: boolean
}

const initialSnap: CatalogSnap = { enabled: false, tools: [], settled: true }
let snap: CatalogSnap = initialSnap
let generation = 0
let refs = 0
let timer: ReturnType<typeof setInterval> | undefined
const listeners = new Set<() => void>()

function emit() {
  for (const listener of listeners) listener()
}

function publish(next: CatalogSnap) {
  if (
    next.enabled === snap.enabled &&
    next.settled === snap.settled &&
    next.tools === snap.tools
  ) {
    return
  }
  snap = next
  emit()
}

async function pull(mode: "replace" | "append", before?: string, onlyId?: string) {
  const gen = generation
  try {
    const cat = await api.clients(before)
    if (gen !== generation) return
    if (cat.pending && mode === "replace") {
      publish({ ...snap, enabled: cat.enabled, settled: true })
      return
    }
    const incoming = (cat.tools ?? []).filter((item) => !onlyId || item.id === onlyId)
    const tools =
      mode === "replace" && !cat.enabled && snap.tools.length === 0 && incoming.length === 0
        ? snap.tools
        : mergeClientTools(snap.tools, incoming, mode)
    publish({ enabled: mode === "append" ? snap.enabled : cat.enabled, tools, settled: true })
  } catch {
    if (gen !== generation) return
    publish({ ...snap, settled: true })
  }
}

function ensurePoll() {
  if (timer) return
  const tick = () => {
    void pull("replace")
  }
  tick()
  timer = setInterval(tick, POLL_MS)
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

/** One poll for the rail and the list. Two intervals used to race More. */
export function useClientCatalog(): CatalogSnap {
  const current = useSyncExternalStore(subscribe, () => snap, () => initialSnap)
  useEffect(() => {
    refs += 1
    ensurePoll()
    return () => {
      refs -= 1
      if (refs <= 0) {
        refs = 0
        if (timer) clearInterval(timer)
        timer = undefined
      }
    }
  }, [])
  return current
}

export function resetClientCatalog() {
  generation += 1
  snap = initialSnap
  emit()
}

export function loadMoreClients(tool: ClientTool) {
  if (!tool.next) return Promise.resolve()
  return pull("append", tool.next, tool.id)
}

/** Read-only tool groups. A row opens the session in the task column.
 *  The composer stays on screen and cannot send. */
export function LocalClientsSection({ bare = false }: { bare?: boolean }) {
  const t = useT()
  const catalog = useClientCatalog()
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const reading = useOpenClient()
  const toggle = (id: string) => setOpen((cur) => ({ ...cur, [id]: cur[id] === false }))

  if (!catalog.enabled) return null

  const groups = catalog.tools.map((tool) => (
    <SidebarSection
      key={tool.id}
      testId={`client-tool-${tool.id}`}
      label={t(toolLabelKey(tool.id))}
      open={open[tool.id] !== false}
      onToggle={() => toggle(tool.id)}
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
          onClick={() => void loadMoreClients(tool)}
        >
          {t("sidebar.clientMore")}
        </Button>
      ) : null}
    </SidebarSection>
  ))

  if (bare) {
    return <div data-testid="clients-list">{groups}</div>
  }

  return (
    <SidebarSection
      testId="clients-list"
      label={t("sidebar.clients")}
      open={open.clients !== false}
      onToggle={() => toggle("clients")}
    >
      {groups}
    </SidebarSection>
  )
}

/** Nothing picked yet. The list is the rail's job; this column stays the task. */
export function ClientsStage() {
  const t = useT()
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center p-6">
      <p className="max-w-sm text-center text-sm text-muted-foreground">
        {t("sidebar.clientStageEmpty")}
      </p>
    </div>
  )
}
