import { Wrench } from "lucide-react"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { api } from "@/lib/api"
import { composerToolsMenuPlacement } from "@/lib/composer-menu"
import type { MCPServerView } from "@/lib/mcp-settings"
import { catalogToolOn, mcpToolOn, withCatalogTool, withMCPTool } from "@/lib/tool-toggle"
import type { Settings, ToolDescriptor } from "@/lib/types"
import { useT } from "@/lib/use-t"

const groups = ["files", "shell", "web"] as const

type RemoteCatalog = { status: "loading" | "ready" | "error"; tools: Array<{ name: string }> }

/** Built-in tools and each remote MCP tool, sent on the next model request. */
export function ComposerTools({ disabled }: { disabled?: boolean }) {
  const t = useT()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
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
          const tools = (found.tools ?? []).flatMap((tool) => tool.name ? [{ name: tool.name }] : [])
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

  const groupTitle = (group: string) => {
    if (group === "files") return t("composer.toolsFiles")
    if (group === "shell") return t("composer.toolsShell")
    if (group === "web") return t("composer.toolsWeb")
    return group
  }

  const enabledServers = servers.filter((server) => server.enabled)

  return (
    <DropdownMenu open={open} onOpenChange={(next) => {
      if (next) measure()
      setOpen(next)
      if (next) load()
      else stopInspects()
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
        className="z-[80] w-72 overflow-y-auto"
      >
        <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsHint")}</p>
        {error ? <p role="alert" className="px-2 pb-1 text-xs text-destructive">{error}</p> : null}
        {loading && catalog.length === 0 ? (
          <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsLoading")}</p>
        ) : null}
        {groups.map((group) => {
          const rows = catalog.filter((tool) => tool.group === group)
          if (rows.length === 0 || !toolSettings) return null
          return (
            <div key={group}>
              <DropdownMenuSeparator />
              <DropdownMenuLabel>{groupTitle(group)}</DropdownMenuLabel>
              {rows.map((tool) => (
                <DropdownMenuCheckboxItem
                  key={tool.name}
                  checked={catalogToolOn(tool, toolSettings)}
                  onCheckedChange={(on) => toggleTool(tool, on)}
                  onSelect={(event) => event.preventDefault()}
                  aria-label={`${tool.title} (${tool.name})`}
                >
                  <span className="min-w-0">
                    <span className="block">{tool.title}</span>
                    <span className="block font-mono text-muted-foreground">{tool.name}</span>
                  </span>
                </DropdownMenuCheckboxItem>
              ))}
            </div>
          )
        })}
        <DropdownMenuSeparator />
        <DropdownMenuLabel>{t("composer.toolsMCP")}</DropdownMenuLabel>
        {servers.length === 0 && !loading ? (
          <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsNoMCP")}</p>
        ) : null}
        {enabledServers.map((server) => {
          const found = remote[server.id]
          const label = server.name || server.id
          return (
            <div key={server.id}>
              <DropdownMenuLabel className="text-foreground">{label}</DropdownMenuLabel>
              {found?.status === "loading" ? (
                <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsLoading")}</p>
              ) : null}
              {found?.status === "error" ? (
                <p role="alert" className="px-2 py-1.5 text-xs text-destructive">{t("composer.toolsMCPFailed")}</p>
              ) : null}
              {found?.status === "ready" && found.tools.length === 0 ? (
                <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsMCPEmpty")}</p>
              ) : null}
              {found?.tools.map((tool) => (
                <DropdownMenuCheckboxItem
                  key={`${server.id}:${tool.name}`}
                  checked={mcpToolOn(server.disabled_tools, tool.name)}
                  onCheckedChange={(on) => toggleRemote(server, tool.name, on)}
                  onSelect={(event) => event.preventDefault()}
                  aria-label={`${label} (${tool.name})`}
                >
                  <span className="block truncate font-mono">{tool.name}</span>
                </DropdownMenuCheckboxItem>
              ))}
            </div>
          )
        })}
        {servers.some((server) => !server.enabled) ? (
          <p className="px-2 py-1.5 text-xs text-muted-foreground">{t("composer.toolsMCPOff")}</p>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
