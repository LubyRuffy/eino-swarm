import { ChevronDown, ChevronRight, Loader2, Pencil, Plus, Plug, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Disclosure } from "@/components/ui/collapsible"
import { useT } from "@/lib/use-t"
import { useMCPSettings } from "@/store/mcp-settings"
import { ConfirmDeleteDialog } from "./confirm-delete-dialog"
import { MCPServerEditor } from "./mcp-server-editor"
import { SettingsPage, SettingsSection, settingsHintClass, settingsMatch } from "./settings-field"

export function MCPTab({ query = "" }: { query?: string }) {
  const t = useT()
  const state = useMCPSettings()
  return (
    <SettingsPage title={t("mcp.title")} description={t("mcp.description")}>
      <SettingsSection title={t("mcp.servers")} action={
        <Button type="button" variant="outline" size="sm" disabled={!!state.busy || !!state.draft || state.loading || !!state.loadError}
          onClick={() => state.edit()}><Plus className="size-4" />{t("mcp.add")}</Button>
      }>
        {state.loading ? <p role="status" data-settings-row="" className="px-4 py-4 text-sm text-muted-foreground">{t("mcp.loading")}</p> :
          state.loadError ? <div role="alert" data-settings-row="" className="flex items-center justify-between gap-3 px-4 py-3">
            <p className="text-sm text-destructive">{t("mcp.loadFailed")}: {state.loadError}</p>
            <Button type="button" variant="outline" onClick={() => void state.load()}>{t("mcp.retry")}</Button>
          </div> : state.servers.length === 0 ?
            <p data-settings-row="" className="px-4 py-6 text-sm text-muted-foreground">{t("mcp.empty")}</p> :
            state.servers.filter((server) => settingsMatch(query, "mcp", server.name, server.id, server.transport)).map((server) => {
              const connection = state.connections[server.id]
              const checking = state.busy === `inspect:${server.id}`
              return <div key={server.id} data-settings-row="" className="flex flex-wrap items-center gap-3 px-4 py-4">
                <div className="min-w-0 flex-1">
                  <p className="break-words text-sm font-medium">{server.name}</p>
                  <p className={settingsHintClass}>{server.id} · {server.transport}</p>
                  <p role="status" className={settingsHintClass}>
                    {checking ? t("mcp.checking") : connection?.error ? t("mcp.connectionFailed") :
                      connection?.catalog ? t("mcp.connected", { count: connection.catalog.tools.length }) : t("mcp.notChecked")}
                  </p>
                </div>
                <Badge variant={server.enabled ? "outline" : "default"}>{t(server.enabled ? "mcp.enabled" : "mcp.disabled")}</Badge>
                <Button type="button" size="icon" variant="ghost" disabled={!!state.busy || !!state.draft}
                  aria-label={t("mcp.inspectName", { name: server.name })} title={t("mcp.inspect")}
                  onClick={() => void state.inspect(server.id)}>
                  {checking ? <Loader2 className="size-4 animate-spin" /> : <Plug className="size-4" />}
                </Button>
                <Button type="button" size="icon" variant="ghost" disabled={!!state.busy || !!state.draft}
                  aria-label={t("mcp.edit", { name: server.name })} onClick={() => state.edit(server)}><Pencil className="size-4" /></Button>
                <Button type="button" size="icon" variant="ghost" disabled={!!state.busy || !!state.draft}
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
              <h4 className="text-sm font-medium">{server.name}</h4>
              {connection.error ? <p role="alert" className="mt-2 break-words text-sm text-destructive">{connection.error}</p> :
                connection.catalog ? <>
                  {connection.catalog.server_name ? <p className={settingsHintClass}>{connection.catalog.server_name} {connection.catalog.server_version}</p> : null}
                  {connection.catalog.tools.length === 0 ? <p className={settingsHintClass}>{t("mcp.noTools")}</p> :
                    <div className="mt-3 divide-y divide-border">{connection.catalog.tools.map((tool) => <Disclosure key={tool.name} className="py-2"
                      open={!!state.expandedTools[`${server.id}:${tool.name}`]}
                      onOpenChange={(open) => state.setToolExpanded(`${server.id}:${tool.name}`, open)}
                      summary={<>{state.expandedTools[`${server.id}:${tool.name}`] ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}<span className="font-mono">{tool.original_name ?? tool.name}</span></>}>
                      {tool.original_name ? <p className="mt-2 break-all font-mono text-xs text-muted-foreground">{tool.name}</p> : null}
                      {tool.description ? <p className="mt-2 whitespace-pre-wrap break-words text-sm text-muted-foreground">{tool.description}</p> : null}
                      <p className="mt-3 text-xs text-muted-foreground">{t("mcp.schema")}</p>
                      <pre className="mt-1 max-h-64 overflow-auto rounded-md bg-muted p-3 font-mono text-xs">{JSON.stringify(tool.input_schema ?? {}, null, 2)}</pre>
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
  )
}
