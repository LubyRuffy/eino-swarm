import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { SelectItem } from "@/components/ui/select"
import { catalogToolOn, withCatalogTool } from "@/lib/tool-toggle"
import type { Settings, ToolDescriptor } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"

import {
  Field,
  SettingsPage,
  SettingsChoice,
  SettingsSection,
  settingsHintClass,
  settingsLabelClass,
  settingsMatch,
  settingsRowClass,
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
  const isOn = (t: ToolDescriptor) => catalogToolOn(t, settings.tools)

  const toggle = (t: ToolDescriptor, on: boolean) => {
    onChange({ ...settings, tools: withCatalogTool(t, on, settings.tools) })
  }

  const groups = [...new Set(catalog.map((t) => t.group))]

  return (
    <SettingsPage
      title={tr("settings.tools.title")}
      description={tr("settings.tools.desc")}
    >
      <SettingsSection title={tr("settings.tools.execution")}>
        <SettingsChoice
          query={query}
          label={tr("settings.tools.executionLocation")}
          hint={tr("settings.tools.executionHint")}
          search={["web_search", "web_fetch", "execution", "cloud", "local"]}
          value={settings.tools.web_execution ?? "auto"}
          onValueChange={(value) => onChange({
            ...settings,
            tools: { ...settings.tools, web_execution: value === "local" || value === "cloud" ? value : "auto" },
          })}
        >
          <SelectItem value="auto">{tr("settings.tools.executionAuto")}</SelectItem>
          <SelectItem value="local">{tr("settings.tools.executionLocal")}</SelectItem>
          <SelectItem value="cloud">{tr("settings.tools.executionCloud")}</SelectItem>
        </SettingsChoice>
      </SettingsSection>
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
                className={cn(
                  settingsRowClass,
                  "cursor-pointer hover:bg-accent/40",
                )}
              >
                <div className="min-w-0 flex-1">
                  <p className={settingsLabelClass}>
                    <span className="font-mono">{t.name}</span>
                    <span className="text-muted-foreground"> — {t.title}</span>
                  </p>
                  <p className={settingsHintClass}>{t.summary}</p>
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

      {settings.tools.web_execution === "cloud" ? null : (
      <SettingsSection
        title={tr("settings.tools.proxy")}
        description={tr("settings.tools.proxyDesc")}
      >
        <Field query={query} label={tr("settings.tools.http")} wide>
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
        <Field query={query} label={tr("settings.tools.https")} wide>
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
          wide
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
      )}

      <SettingsSection title={tr("settings.tools.limits")}>
        <Field
          query={query}
          label={tr("settings.tools.resultMax")}
          hint={tr("settings.tools.resultMaxHint")}
        >
          <Input
            type="number"
            min={1000}
            value={settings.tools.result_max_chars ?? 40000}
            onChange={(e) =>
              onChange({
                ...settings,
                tools: { ...settings.tools, result_max_chars: Number(e.target.value) },
              })
            }
          />
        </Field>
      </SettingsSection>
    </SettingsPage>
  )
}
