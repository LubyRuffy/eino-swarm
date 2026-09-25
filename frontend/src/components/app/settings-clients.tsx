import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { defaultClientsSettings, type Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import { Field, SettingsPage, SettingsSection } from "./settings-field"

export function ClientsTab({
  settings,
  onChange,
  query,
}: {
  settings: Settings
  onChange: (next: Settings) => void
  query: string
}) {
  const t = useT()
  const clients = defaultClientsSettings(settings.clients)
  const update = (patch: Partial<typeof clients>) => {
    onChange({ ...settings, clients: { ...clients, ...patch } })
  }
  return (
    <SettingsPage title={t("settings.clients.title")} description={t("settings.clients.desc")}>
      <SettingsSection title={t("settings.nav.clients")}>
        <Field
          query={query}
          label={t("settings.clients.enabled")}
          hint={t("settings.clients.enabledHint")}
        >
          <Switch
            checked={clients.enabled}
            onCheckedChange={(v) => update({ enabled: v })}
            aria-label={t("settings.clients.enabled")}
          />
        </Field>
        <Field query={query} label={t("settings.clients.claudeDir")} wide>
          <Input
            aria-label={t("settings.clients.claudeDir")}
            value={clients.claude_dir}
            onChange={(e) => update({ claude_dir: e.target.value })}
            spellCheck={false}
            autoComplete="off"
          />
        </Field>
        <Field query={query} label={t("settings.clients.codexDir")} wide>
          <Input
            aria-label={t("settings.clients.codexDir")}
            value={clients.codex_dir}
            onChange={(e) => update({ codex_dir: e.target.value })}
            spellCheck={false}
            autoComplete="off"
          />
        </Field>
        <Field query={query} label={t("settings.clients.cursorDir")} wide>
          <Input
            aria-label={t("settings.clients.cursorDir")}
            value={clients.cursor_dir}
            onChange={(e) => update({ cursor_dir: e.target.value })}
            spellCheck={false}
            autoComplete="off"
          />
        </Field>
      </SettingsSection>
    </SettingsPage>
  )
}
