import type { ReactNode } from "react"
import { Braces, ChevronDown, ChevronRight, Circle, Loader2, Pencil, Plus, Plug, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Disclosure } from "@/components/ui/collapsible"
import { HighlightedLines } from "@/lib/highlighted-lines"
import { mcpTransportKey, type MCPServerView } from "@/lib/mcp-settings"
import { useT, type Translate } from "@/lib/use-t"
import { cn } from "@/lib/utils"
import { useMCPSettings } from "@/store/mcp-settings"
import { ConfirmDeleteDialog } from "./confirm-delete-dialog"
import { MCPJSONEditor } from "./mcp-json-editor"
import { MCPServerEditor } from "./mcp-server-editor"
import { SettingsPage, SettingsRow, SettingsSection, settingsHintClass, settingsMatch } from "./settings-field"

/** Search words for connections, transports and the old MCP page. */
export const mcpSettingsSearch =
  "mcp server external tools stdio streamable http sse connection command environment headers MCP 服务 外部工具 连接 命令 环境变量 请求头"

type MCPMark = "on" | "off" | "connected" | "failed" | "checking" | "unchecked"

function mcpBlockVisible(query: string, servers: MCPServerView[], t: Translate) {
  if (settingsMatch(
    query,
    mcpSettingsSearch,
    t("mcp.title"),
    t("mcp.description"),
    t("mcp.directLoad"),
    t("mcp.directLoadHint"),
    t("mcp.servers"),
    t("mcp.empty"),
    t("mcp.catalog"),
    t("mcp.catalogHint"),
  )) return true
  return servers.some((server) => settingsMatch(query, server.name, t(mcpTransportKey(server.transport))))
}

function markTone(mark: MCPMark) {
  if (mark === "on" || mark === "connected") return "text-done"
  if (mark === "failed") return "text-destructive"
  if (mark === "checking") return "text-running"
  return "text-muted-foreground"
}

function StatusMark({ mark }: { mark: MCPMark }) {
  const tone = markTone(mark)
  if (mark === "checking") {
    return <Loader2 aria-hidden data-mcp-status={mark} className={cn("size-3 shrink-0 animate-spin", tone)} />
  }
  return <Circle aria-hidden data-mcp-status={mark} className={cn("size-2.5 shrink-0 fill-current", tone)} />
}

export function MCPTab({ query = "", leading }: { query?: string; leading?: ReactNode }) {
  const t = useT()
  const state = useMCPSettings()
  const locked = !!state.busy || !!state.draft || state.jsonText !== undefined
  // The JSON document is the whole right-hand pane. Leaving the tools list
  // above it collapses the editor.
  if (state.jsonText !== undefined) {
    return <MCPJSONEditor text={state.jsonText} saving={state.busy === "json"} error={state.formError}
      onChange={state.setJsonText} onApply={() => void state.applyJSON()} onCancel={state.cancelJSON} />
  }
  return (
    <>
    {leading}
    {mcpBlockVisible(query, state.servers, t) ? (
    <SettingsPage title={t("mcp.title")} description={t("mcp.description")}>
      <SettingsSection title={t("mcp.directLoad")}>
        <SettingsRow label={t("mcp.directLoad")} hint={t("mcp.directLoadHint")} htmlFor="mcp-direct-load">
          <Input id="mcp-direct-load" type="number" min={0} max={100} inputMode="numeric"
            value={state.directLoadPercent}
            onChange={(event) => state.setDirectLoadPercent(Number(event.target.value))}
            onBlur={(event) => void state.saveDirectLoad(Number(event.currentTarget.value))} />
        </SettingsRow>
      </SettingsSection>
      <SettingsSection title={t("mcp.servers")} action={
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" disabled={locked || state.loading || !!state.loadError}
            onClick={() => state.openJSON()}><Braces className="size-4" />{t("mcp.json")}</Button>
          <Button type="button" variant="outline" size="sm" disabled={locked || state.loading || !!state.loadError}
            onClick={() => state.edit()}><Plus className="size-4" />{t("mcp.add")}</Button>
        </div>
      }>
        {state.loading ? <p role="status" data-settings-row="" className="px-4 py-4 text-(length:--chrome-font-size) text-muted-foreground">{t("mcp.loading")}</p> :
          state.loadError ? <div role="alert" data-settings-row="" className="flex items-center justify-between gap-3 px-4 py-3">
            <p className="text-(length:--chrome-font-size) text-destructive">{t("mcp.loadFailed")}: {state.loadError}</p>
            <Button type="button" variant="outline" onClick={() => void state.load()}>{t("mcp.retry")}</Button>
          </div> : state.servers.length === 0 ?
            <p data-settings-row="" className="px-4 py-6 text-(length:--chrome-font-size) text-muted-foreground">{t("mcp.empty")}</p> :
            state.servers.filter((server) => settingsMatch(query, "mcp", server.name, t(mcpTransportKey(server.transport)))).map((server) => {
              const connection = state.connections[server.id]
              const checking = state.busy === `inspect:${server.id}`
              const connectionMark: MCPMark = checking ? "checking" : connection?.error ? "failed" : connection?.catalog ? "connected" : "unchecked"
              const connectionLabel = checking ? t("mcp.checking") : connection?.error ? t("mcp.connectionFailed") :
                connection?.catalog ? t("mcp.connected", { count: connection.catalog.tools.length }) : t("mcp.notChecked")
              return <div key={server.id} data-settings-row="" className="flex flex-wrap items-center gap-3 px-4 py-4">
                <div className="min-w-0 flex-1">
                  <p className="break-words text-(length:--chrome-font-size) font-medium">{server.name}</p>
                  <p className={settingsHintClass}>{t(mcpTransportKey(server.transport))}</p>
                  <p role="status" className={cn(settingsHintClass, "flex items-center gap-1.5", markTone(connectionMark))}>
                    <StatusMark mark={connectionMark} />
                    {connectionLabel}
                  </p>
                </div>
                <Badge variant={server.enabled ? "success" : "default"}>
                  <StatusMark mark={server.enabled ? "on" : "off"} />
                  {t(server.enabled ? "mcp.enabled" : "mcp.disabled")}
                </Badge>
                <Button type="button" size="icon" variant="ghost" disabled={locked}
                  aria-label={t("mcp.inspectName", { name: server.name })} title={t("mcp.inspect")}
                  onClick={() => void state.inspect(server.id)}>
                  {checking ? <Loader2 className="size-4 animate-spin" /> : <Plug className="size-4" />}
                </Button>
                <Button type="button" size="icon" variant="ghost" disabled={locked}
                  aria-label={t("mcp.edit", { name: server.name })} onClick={() => state.edit(server)}><Pencil className="size-4" /></Button>
                <Button type="button" size="icon" variant="ghost" disabled={locked}
                  aria-label={t("mcp.remove", { name: server.name })} onClick={() => state.setPendingDelete(server)}><Trash2 className="size-4" /></Button>
              </div>
            })}
      </SettingsSection>

      {state.draft ? <MCPServerEditor draft={state.draft} saved={state.servers.find((server) => server.id === state.editing)}
        saving={state.busy === "save"} error={state.formError} onChange={state.setDraft}
        onSave={() => void state.save()} onCancel={state.cancel} /> : null}

      {Object.keys(state.connections).length === 0 ? null : (
      <SettingsSection title={t("mcp.catalog")} description={t("mcp.catalogHint")}>
        {state.servers.filter((server) => state.connections[server.id]).map((server) => {
            const connection = state.connections[server.id]
            return <section key={server.id} aria-label={`${server.name} ${t("mcp.catalog")}`} data-settings-row="" className="px-4 py-4">
              <h4 className="text-(length:--chrome-font-size) font-medium">{server.name}</h4>
              {connection.error ? <p role="alert" className="mt-2 break-words text-(length:--chrome-font-size) text-destructive">{connection.error}</p> :
                connection.catalog ? <>
                  {connection.catalog.server_name ? <p className={settingsHintClass}>{connection.catalog.server_name} {connection.catalog.server_version}</p> : null}
                  {connection.catalog.tools.length === 0 ? <p className={settingsHintClass}>{t("mcp.noTools")}</p> :
                    <div className="mt-3 divide-y divide-border">{connection.catalog.tools.map((tool) => <Disclosure key={tool.name} className="py-2"
                      open={!!state.expandedTools[`${server.id}:${tool.name}`]}
                      onOpenChange={(open) => state.setToolExpanded(`${server.id}:${tool.name}`, open)}
                      summary={<>{state.expandedTools[`${server.id}:${tool.name}`] ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}<span className="font-mono">{tool.original_name ?? tool.name}</span></>}>
                      {tool.original_name ? <p className="mt-2 break-all font-mono text-xs text-muted-foreground">{tool.name}</p> : null}
                      {tool.description ? <p className="mt-2 whitespace-pre-wrap break-words text-(length:--chrome-font-size) text-muted-foreground">{tool.description}</p> : null}
                      <p className="mt-3 text-xs text-muted-foreground">{t("mcp.schema")}</p>
                      <pre className="mt-1 max-h-64 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">
                        <HighlightedLines text={JSON.stringify(tool.input_schema ?? {}, null, 2)} lang="json" />
                      </pre>
                    </Disclosure>)}</div>}
                </> : <p className={settingsHintClass}>{t("mcp.checking")}</p>}
            </section>
          })}
      </SettingsSection>
      )}
      <ConfirmDeleteDialog open={!!state.pendingDelete} title={t("mcp.removeConfirm")} description={t("mcp.removeDescription")}
        confirmLabel={t("mcp.delete")} cancelLabel={t("mcp.cancel")}
        onOpenChange={(open) => { if (!open) state.setPendingDelete(undefined) }}
        onConfirm={() => { if (state.pendingDelete) void state.remove(state.pendingDelete.id) }} />
    </SettingsPage>
    ) : null}
    </>
  )
}
