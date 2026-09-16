import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import type { Settings, ToolDescriptor } from "@/lib/types"
import { useT } from "@/lib/use-t"

import {
  Field,
  SettingsPage,
  SettingsSection,
  settingsMatch,
} from "./settings-field"

export function ToolsTab({
  settings,
  catalog,
  onChange,
  query = "",
}: {
  settings: Settings
  catalog: ToolDescriptor[]
  onChange: (s: Settings) => void
  query?: string
}) {
  const tr = useT()
  // The config records exceptions rather than the whole list, so a tool added
  // in a later release keeps its own default instead of silently arriving off.
  const isOn = (t: ToolDescriptor) =>
    t.default_off
      ? settings.tools.enabled.includes(t.name)
      : !settings.tools.disabled.includes(t.name)

  const toggle = (t: ToolDescriptor, on: boolean) => {
    const disabled = new Set(settings.tools.disabled)
    const enabled = new Set(settings.tools.enabled)
    if (t.default_off) {
      on ? enabled.add(t.name) : enabled.delete(t.name)
    } else {
      on ? disabled.delete(t.name) : disabled.add(t.name)
    }
    onChange({
      ...settings,
      tools: {
        ...settings.tools,
        disabled: [...disabled],
        enabled: [...enabled],
      },
    })
  }

  const groups = [...new Set(catalog.map((t) => t.group))]

  return (
    <SettingsPage
      title={tr("settings.tools.title")}
      description={tr("settings.tools.desc")}
    >
      {groups.map((group) => (
        <SettingsSection key={group} title={group.charAt(0).toUpperCase() + group.slice(1)}>
          {catalog
            .filter((t) => t.group === group)
            .filter((t) =>
              settingsMatch(query, t.name, t.title, t.summary, t.group),
            )
            .map((t) => (
              <label
                key={t.name}
                data-settings-row=""
                className="flex cursor-pointer items-start justify-between gap-6 px-4 py-3.5 hover:bg-accent/40"
              >
                <div className="min-w-0">
                  <p className="text-sm">
                    <span className="font-mono text-[13px]">{t.name}</span>
                    <span className="text-muted-foreground"> — {t.title}</span>
                  </p>
                  <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                    {t.summary}
                  </p>
                </div>
                <Switch
                  checked={isOn(t)}
                  onCheckedChange={(on) => toggle(t, on)}
                  className="mt-0.5"
                />
              </label>
            ))}
        </SettingsSection>
      ))}

      <SettingsSection
        title={tr("settings.tools.proxy")}
        description={tr("settings.tools.proxyDesc")}
      >
        <Field query={query} label={tr("settings.tools.http")}>
          <Input
            value={settings.tools.proxy.http}
            placeholder="http://127.0.0.1:7890"
            onChange={(e) =>
              onChange({
                ...settings,
                tools: {
                  ...settings.tools,
                  proxy: { ...settings.tools.proxy, http: e.target.value },
                },
              })
            }
          />
        </Field>
        <Field query={query} label={tr("settings.tools.https")}>
          <Input
            value={settings.tools.proxy.https}
            onChange={(e) =>
              onChange({
                ...settings,
                tools: {
                  ...settings.tools,
                  proxy: { ...settings.tools.proxy, https: e.target.value },
                },
              })
            }
          />
        </Field>
        <Field
          query={query}
          label={tr("settings.tools.noProxy")}
          hint={tr("settings.tools.noProxyHint")}
        >
          <Input
            value={settings.tools.proxy.no_proxy}
            placeholder="localhost,127.0.0.1"
            onChange={(e) =>
              onChange({
                ...settings,
                tools: {
                  ...settings.tools,
                  proxy: { ...settings.tools.proxy, no_proxy: e.target.value },
                },
              })
            }
          />
        </Field>
      </SettingsSection>
    </SettingsPage>
  )
}
