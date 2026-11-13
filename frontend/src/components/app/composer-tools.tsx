import { ChevronRight, Info, Wrench } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { api } from "@/lib/api"
import { composerToolsMenuPlacement, filterRemoteTools, toolHoverText, toolMenuMatches, toolMenuQuery } from "@/lib/composer-menu"
import { cn } from "@/lib/utils"
import type { MCPServerView } from "@/lib/mcp-settings"
import { catalogToolOn, mcpToolOn, withCatalogTool, withMCPTool } from "@/lib/tool-toggle"
import type { Settings, ToolDescriptor } from "@/lib/types"
import { useT } from "@/lib/use-t"

type RemoteTool = { name: string; description: string }
type RemoteCatalog = { status: "loading" | "ready" | "error"; tools: RemoteTool[] }

function ToolHint({ label, text }: { label: string; text: string }) {
  if (!text.trim()) return null
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          aria-label={label}
          className="shrink-0 rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
          onPointerDown={(event) => event.stopPropagation()}
          onPointerUp={(event) => event.stopPropagation()}
          onClick={(event) => event.stopPropagation()}
          onKeyDown={(event) => {
            if (event.key !== "Escape") event.stopPropagation()
          }}
        >
          <Info className="size-3" />
        </button>
      </TooltipTrigger>
      <TooltipContent side="top" className="z-[90] max-w-64 whitespace-pre-line text-left">{text}</TooltipContent>
    </Tooltip>
  )
}

function GroupToggle({ label, open, onToggle }: { label: string; open: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      aria-expanded={open}
      className="flex w-full items-center gap-1 rounded-sm px-2 py-1.5 text-left text-xs font-medium text-foreground hover:bg-accent"
      onClick={onToggle}
      onKeyDown={(event) => {
        if (event.key !== "Escape") event.stopPropagation()
      }}
    >
      <ChevronRight className={cn("size-3.5 shrink-0 transition-transform", open && "rotate-90")} />
      <span className="truncate">{label}</span>
    </button>
  )
}

/** Built-in tools and each remote MCP tool, sent on the next model request. */
export function ComposerTools({ disabled }: { disabled?: boolean }) {
  const t = useT()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const filterRef = useRef<HTMLInputElement>(null)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [folded, setFolded] = useState<Record<string, boolean>>({})
  const [placement, setPlacement] = useState({ sideOffset: 8, maxHeight: 320 })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const [catalog, setCatalog] = useState<ToolDescriptor[]>([])
  const [toolSettings, setToolSettings] = useState<Settings["tools"]>()
  const [servers, setServers] = useState<MCPServerView[]>([])
  const [remote, setRemote] = useState<Record<string, RemoteCatalog>>({})
  const revision = useRef(0)
  const inspectRevision = useRef(0)
  const writes = useRef(Promise.resolve())
  const inspects = useRef<AbortController[]>([])

  const stopInspects = () => {
    inspects.current.forEach((controller) => controller.abort())
    inspects.current = []
  }

  useEffect(() => stopInspects, [])

  const measure = () => {
    const button = triggerRef.current
    const box = button?.closest("[data-composer-box]")
    if (!button || !(box instanceof HTMLElement)) return
    setPlacement(composerToolsMenuPlacement(button.getBoundingClientRect(), box.getBoundingClientRect()))
  }

  useEffect(() => {
    if (!open) return
    filterRef.current?.focus()
    measure()
    const box = triggerRef.current?.closest("[data-composer-box]")
    if (!box) return
    const observer = new ResizeObserver(measure)
    observer.observe(box)
    window.addEventListener("resize", measure)
    return () => {
      observer.disconnect()
      window.removeEventListener("resize", measure)
    }
  }, [open])

  const enqueue = (job: () => Promise<void>) => {
    const run = writes.current.then(job, job)
    writes.current = run.then(() => undefined, () => undefined)
    return run
  }

  const inspectEnabled = (list: MCPServerView[]) => {
    stopInspects()
    const rev = ++inspectRevision.current
    const enabled = list.filter((server) => server.enabled)
    const initial: Record<string, RemoteCatalog> = {}
    for (const server of enabled) initial[server.id] = { status: "loading", tools: [] }
    setRemote(initial)
    for (const server of enabled) {
      const controller = new AbortController()
      inspects.current.push(controller)
      void api.inspectMCPServer(server.id, controller.signal)
        .then((found) => {
          if (inspectRevision.current !== rev || controller.signal.aborted) return
          const tools = (found.tools ?? []).flatMap((tool) => tool.name
            ? [{ name: tool.name, description: tool.description?.trim() ?? "" }]
            : [])
          setRemote((current) => ({ ...current, [server.id]: { status: "ready", tools } }))
        })
        .catch(() => {
          if (inspectRevision.current !== rev || controller.signal.aborted) return
          setRemote((current) => ({ ...current, [server.id]: { status: "error", tools: [] } }))
        })
    }
  }

  const load = () => {
    const rev = ++revision.current
    setLoading(true)
    setError("")
    void Promise.all([api.tools(), api.mcpServers(), api.settings()])
      .then(([tools, nextServers, settings]) => {
        if (revision.current !== rev) return
        setCatalog(tools.catalog)
        setServers(nextServers)
        setToolSettings(settings.tools)
        inspectEnabled(nextServers)
      })
      .catch(() => {
        if (revision.current === rev) setError(t("composer.toolsLoadFailed"))
      })
      .finally(() => {
        if (revision.current === rev) setLoading(false)
      })
  }

  const toggleTool = (tool: ToolDescriptor, on: boolean) => {
    const rev = ++revision.current
    setToolSettings((current) => current ? withCatalogTool(tool, on, current) : current)
    setError("")
    void enqueue(async () => {
      try {
        const latest = await api.settings()
        const saved = await api.saveSettings({ tools: withCatalogTool(tool, on, latest.tools) })
        if (revision.current === rev) setToolSettings(saved.tools)
      } catch {
        setError(t("composer.toolsSaveFailed"))
        const fresh = await api.settings().catch(() => undefined)
        if (fresh && revision.current === rev) setToolSettings(fresh.tools)
      }
    })
  }

  const toggleRemote = (server: MCPServerView, name: string, on: boolean) => {
    const rev = ++revision.current
    setServers((current) => current.map((item) => item.id === server.id
      ? { ...item, disabled_tools: withMCPTool(item.disabled_tools, name, on) }
      : item))
    setError("")
    void enqueue(async () => {
      try {
        const latest = await api.mcpServers()
        const current = latest.find((item) => item.id === server.id)
        if (!current) throw new Error("missing")
        const saved = await api.saveMCPServer({
          id: current.id, name: current.name, transport: current.transport,
          command: current.command, args: current.args, url: current.url,
          enabled: current.enabled, timeout_seconds: current.timeout_seconds,
          disabled_tools: withMCPTool(current.disabled_tools, name, on),
        })
        if (revision.current === rev) {
          setServers((list) => list.map((item) => item.id === saved.id ? saved : item))
        }
      } catch {
        setError(t("composer.toolsSaveFailed"))
        const fresh = await api.mcpServers().catch(() => undefined)
        if (fresh && revision.current === rev) setServers(fresh)
      }
    })
  }

  const groupOpen = (id: string, fallback: boolean) => {
    if (Object.hasOwn(folded, id)) return folded[id]
    if (toolMenuQuery(query) !== "") return true
    return fallback
  }
  const toggleGroup = (id: string, fallback: boolean) => {
    setFolded((current) => {
      const open = Object.hasOwn(current, id) ? current[id] : toolMenuQuery(query) !== "" || fallback
      return { ...current, [id]: !open }
    })
  }

  const builtin = catalog.filter((tool) => toolMenuMatches(query, [tool.title, tool.name]))
  const enabledServers = servers.filter((server) => server.enabled)
  const waiting = enabledServers.some((server) => remote[server.id]?.status === "loading")
  const remoteGroups = enabledServers.flatMap((server) => {
    const found = remote[server.id]
    const label = server.name || server.id
    const filtered = filterRemoteTools(found?.tools ?? [], query, label, server.id)
    const pending = found?.status !== "ready"
    const showPending = pending && (toolMenuQuery(query) === "" || toolMenuMatches(query, [label, server.id]))
    if (!filtered.shown && !showPending) return []
    return [{ server, label, found, tools: filtered.tools }]
  })
  const showNoMatch = toolMenuQuery(query) !== "" && builtin.length === 0 && remoteGroups.length === 0 && !waiting && !loading

  return (
    <DropdownMenu open={open} onOpenChange={(next) => {
      if (next) measure()
      setOpen(next)
      if (next) load()
      else {
        stopInspects()
        setQuery("")
      }
    }}>
      <DropdownMenuTrigger asChild>
        <Button
          ref={triggerRef}
          variant="ghost"
          size="sm"
          disabled={disabled}
          data-testid="composer-tools"
          aria-label={t("composer.tools")}
          title={t("composer.toolsHint")}
          className="h-7 gap-1.5 px-2"
        >
          <Wrench />
          {t("composer.tools")}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="top"
        align="start"
        avoidCollisions={false}
        sideOffset={placement.sideOffset}
        style={{ maxHeight: placement.maxHeight }}
        data-testid="composer-tools-menu"
        className="z-[80] grid w-80 grid-rows-[auto_minmax(0,1fr)] overflow-hidden p-0"
      >
        <div className="space-y-1.5 border-b px-2 py-2">
          <Input
            ref={filterRef}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== "Escape") event.stopPropagation()
            }}
            placeholder={t("composer.toolsSearch")}
            aria-label={t("composer.toolsSearch")}
            autoComplete="off"
            className="h-8 px-2 text-xs"
          />
          <p className="px-0.5 text-xs text-muted-foreground">{t("composer.toolsHint")}</p>
          {error ? <p role="alert" className="px-0.5 text-xs text-destructive">{error}</p> : null}
        </div>
        {/* Reserve the scrollbar so it cannot paint over the switch. */}
        <TooltipProvider delayDuration={200}>
        <div
          data-testid="composer-tools-list"
          className="min-h-0 overflow-y-auto px-1 py-1 pe-3"
          style={{ scrollbarGutter: "stable" }}
        >
          {loading && catalog.length === 0 ? (
            <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsLoading")}</p>
          ) : null}
          {showNoMatch ? (
            <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsNoMatch")}</p>
          ) : null}
          {builtin.length > 0 && toolSettings ? (
            <div role="group" aria-label={t("composer.toolsBuiltin")}>
              <GroupToggle
                label={t("composer.toolsBuiltin")}
                open={groupOpen("builtin", true)}
                onToggle={() => toggleGroup("builtin", true)}
              />
              {groupOpen("builtin", true) ? builtin.map((tool) => (
                <DropdownMenuCheckboxItem
                  key={tool.name}
                  checked={catalogToolOn(tool, toolSettings)}
                  onCheckedChange={(on) => toggleTool(tool, on)}
                  onSelect={(event) => event.preventDefault()}
                  aria-label={`${tool.title} (${tool.name})`}
                  className="w-full min-w-0 py-1 pr-3"
                >
                  <span className="flex min-w-0 items-center gap-1">
                    <span className="truncate">{tool.title}</span>
                    <ToolHint label={t("composer.toolsDetail", { name: tool.title })} text={toolHoverText(tool.name, tool.summary)} />
                  </span>
                </DropdownMenuCheckboxItem>
              )) : null}
            </div>
          ) : null}
          {servers.length === 0 && !loading && toolMenuQuery(query) === "" ? (
            <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsNoMCP")}</p>
          ) : null}
          {remoteGroups.map(({ server, label, found, tools }, index) => {
            const open = groupOpen(server.id, false)
            return (
              <div key={server.id} role="group" aria-label={label}>
                {builtin.length > 0 || index > 0 ? <DropdownMenuSeparator /> : null}
                <GroupToggle label={label} open={open} onToggle={() => toggleGroup(server.id, false)} />
                {open && found?.status === "loading" ? (
                  <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsLoading")}</p>
                ) : null}
                {found?.status === "error" ? (
                  <p role="alert" className="px-2 py-1.5 text-xs text-destructive">{t("composer.toolsMCPFailed")}</p>
                ) : null}
                {open && found?.status === "ready" && found.tools.length === 0 ? (
                  <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsMCPEmpty")}</p>
                ) : null}
                {open ? tools.map((tool) => (
                  <DropdownMenuCheckboxItem
                    key={`${server.id}:${tool.name}`}
                    checked={mcpToolOn(server.disabled_tools, tool.name)}
                    onCheckedChange={(on) => toggleRemote(server, tool.name, on)}
                    onSelect={(event) => event.preventDefault()}
                    aria-label={`${label} (${tool.name})`}
                    className="w-full min-w-0 py-1 pr-3"
                  >
                    <span className="flex min-w-0 items-center gap-1">
                      <span className="truncate font-mono">{tool.name}</span>
                      <ToolHint label={t("composer.toolsDetail", { name: tool.name })} text={tool.description} />
                    </span>
                  </DropdownMenuCheckboxItem>
                )) : null}
              </div>
            )
          })}
          {toolMenuQuery(query) === "" && servers.some((server) => !server.enabled) ? (
            <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsMCPOff")}</p>
          ) : null}
        </div>
        </TooltipProvider>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
