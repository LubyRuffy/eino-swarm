import { Wrench } from "lucide-react"
import { useRef, useState } from "react"

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
import type { MCPServerView } from "@/lib/mcp-settings"
import { catalogToolOn, withCatalogTool } from "@/lib/tool-toggle"
import type { Settings, ToolDescriptor } from "@/lib/types"
import { useT } from "@/lib/use-t"

const groups = ["files", "shell", "web"] as const

/** Built-in tools and configured MCP servers, sent on the next model request. */
export function ComposerTools({ disabled }: { disabled?: boolean }) {
  const t = useT()
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const [catalog, setCatalog] = useState<ToolDescriptor[]>([])
  const [toolSettings, setToolSettings] = useState<Settings["tools"]>()
  const [servers, setServers] = useState<MCPServerView[]>([])
  const revision = useRef(0)
  const writes = useRef(Promise.resolve())

  const enqueue = (job: () => Promise<void>) => {
    const run = writes.current.then(job, job)
    writes.current = run.then(() => undefined, () => undefined)
    return run
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

  const toggleServer = (server: MCPServerView, on: boolean) => {
    const rev = ++revision.current
    setServers((current) => current.map((item) => item.id === server.id ? { ...item, enabled: on } : item))
    setError("")
    void enqueue(async () => {
      try {
        const saved = await api.saveMCPServer({
          id: server.id, name: server.name, transport: server.transport,
          command: server.command, args: server.args, url: server.url,
          enabled: on, timeout_seconds: server.timeout_seconds,
        })
        if (revision.current === rev) {
          setServers((current) => current.map((item) => item.id === saved.id ? saved : item))
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

  return (
    <DropdownMenu open={open} onOpenChange={(next) => {
      setOpen(next)
      if (next) load()
    }}>
      <DropdownMenuTrigger asChild>
        <Button
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
      <DropdownMenuContent side="top" align="start" className="max-h-80 w-72 overflow-y-auto">
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
        ) : servers.map((server) => (
          <DropdownMenuCheckboxItem
            key={server.id}
            checked={server.enabled}
            onCheckedChange={(on) => toggleServer(server, on)}
            onSelect={(event) => event.preventDefault()}
            aria-label={server.name || server.id}
          >
            <span className="min-w-0">
              <span className="block">{server.name || server.id}</span>
              {server.name && server.name !== server.id ? (
                <span className="block font-mono text-muted-foreground">{server.id}</span>
              ) : null}
            </span>
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
