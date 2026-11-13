import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { SelectItem } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import type { MCPDraft, MCPServerView, MCPTransport } from "@/lib/mcp-settings"
import { useT } from "@/lib/use-t"
import { Field, SettingsChoice, SettingsSection } from "./settings-field"

export function MCPServerEditor({ draft, saved, saving, error, onChange, onSave, onCancel }: {
  draft: MCPDraft
  saved?: MCPServerView
  saving: boolean
  error: string
  onChange: (draft: MCPDraft) => void
  onSave: () => void
  onCancel: () => void
}) {
  const t = useT()
  const patch = (next: Partial<MCPDraft>) => onChange({ ...draft, ...next })
  const keysHint = (keys: string[]) => keys.length
    ? t("mcp.savedKeys", { keys: keys.join(", ") }) : t("mcp.noSavedKeys")
  const asTyped = { autoComplete: "off", autoCapitalize: "off", autoCorrect: "off", spellCheck: false } as const

  return (
    <SettingsSection title={t(saved ? "mcp.editTitle" : "mcp.new")}>
      <fieldset disabled={saving} className="min-w-0" data-settings-row="">
        <Field label={t("mcp.name")}>
          <Input type="text" value={draft.name} {...asTyped} onChange={(e) => patch({ name: e.target.value })} />
        </Field>
        <Field label={t("mcp.enabled")}>
          <Switch checked={draft.enabled} onCheckedChange={(enabled) => patch({ enabled })} />
        </Field>
        <SettingsChoice label={t("mcp.transport")} value={draft.transport}
          onValueChange={(value) => patch({ transport: value as MCPTransport })}>
          <SelectItem value="stdio">{t("mcp.stdio")}</SelectItem>
          <SelectItem value="streamable_http">{t("mcp.http")}</SelectItem>
          <SelectItem value="sse">{t("mcp.sse")}</SelectItem>
        </SettingsChoice>
        {draft.transport === "stdio" ? <>
          <Field label={t("mcp.command")} hint={t("mcp.commandHint")}>
            <Input type="text" value={draft.command} {...asTyped} onChange={(e) => patch({ command: e.target.value })} />
          </Field>
          <Field label={t("mcp.args")} hint={t("mcp.argsHint")}>
            <Textarea value={draft.args} rows={3} {...asTyped} className="w-64 font-mono text-xs"
              onChange={(e) => patch({ args: e.target.value })} />
          </Field>
          <Field label={t("mcp.env")} hint={`${t("mcp.secretsHint")} ${keysHint(saved?.env_keys ?? [])}`}>
            <Textarea value={draft.env} rows={3} {...asTyped} className="w-64 font-mono text-xs"
              onChange={(e) => patch({ env: e.target.value })} />
          </Field>
        </> : <>
          <Field label={t("mcp.url")} hint={t("mcp.urlHint")}>
            <Input type="text" value={draft.url} {...asTyped} onChange={(e) => patch({ url: e.target.value })} />
          </Field>
          <Field label={t("mcp.headers")} hint={`${t("mcp.secretsHint")} ${keysHint(saved?.header_keys ?? [])}`}>
            <Textarea value={draft.headers} rows={3} {...asTyped} className="w-64 font-mono text-xs"
              onChange={(e) => patch({ headers: e.target.value })} />
          </Field>
        </>}
        <Field label={t("mcp.timeout")} hint={t("mcp.timeoutHint")}>
          <Input type="number" min={0} max={3600} step={1} value={draft.timeout_seconds} {...asTyped}
            onChange={(e) => patch({ timeout_seconds: Number(e.target.value) })} />
        </Field>
        {error ? <p role="alert" className="px-4 py-3 text-(length:--chrome-font-size) text-destructive">{error}</p> : null}
        <div className="flex justify-end gap-2 px-4 py-3">
          <Button type="button" variant="ghost" onClick={onCancel}>{t("mcp.cancel")}</Button>
          <Button type="button" onClick={onSave}>{t(saving ? "mcp.saving" : "mcp.save")}</Button>
        </div>
      </fieldset>
    </SettingsSection>
  )
}
