import { Loader2, Plus, Trash2 } from "lucide-react"
import { cloneElement, useEffect, useId, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import type { Meta, Settings, ToolDescriptor } from "@/lib/types"
import type { Theme } from "@/store/app"

export function SettingsDialog({
  open,
  onOpenChange,
  meta,
  theme,
  onThemeChange,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  meta?: Meta
  theme: Theme
  onThemeChange: (t: Theme) => void
  onSaved: () => void
}) {
  const [settings, setSettings] = useState<Settings>()
  const [catalog, setCatalog] = useState<ToolDescriptor[]>([])
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    if (!open) return
    setError(undefined)
    void Promise.all([api.settings(), api.tools()])
      .then(([s, t]) => {
        setSettings(s)
        setCatalog(t.catalog)
      })
      .catch((e) => setError(String(e instanceof Error ? e.message : e)))
  }, [open])

  const save = async () => {
    if (!settings) return
    setSaving(true)
    setError(undefined)
    try {
      await api.saveSettings(settings)
      onSaved()
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        {/* A form, so the API key field belongs to one and browsers stop
            warning about a stray password input — and Enter saves. */}
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void save()
          }}
        >
          <DialogHeader>
            <DialogTitle>Settings</DialogTitle>
            <DialogDescription>
              Stored in {meta?.data_dir ?? "the data directory"}/config.yaml
            </DialogDescription>
          </DialogHeader>

          {!settings ? (
            <div className="flex h-64 items-center justify-center text-muted-foreground">
              <Loader2 className="size-5 animate-spin" />
            </div>
          ) : (
            <Tabs defaultValue="models" className="min-h-[26rem]">
              <TabsList>
                <TabsTrigger value="models">Models</TabsTrigger>
                <TabsTrigger value="swarm">Swarm</TabsTrigger>
                <TabsTrigger value="tools">Tools</TabsTrigger>
                <TabsTrigger value="general">General</TabsTrigger>
              </TabsList>

              <TabsContent
                value="models"
                className="thin-scrollbar max-h-96 overflow-y-auto pt-4"
              >
                <ModelsTab settings={settings} onChange={setSettings} />
              </TabsContent>

              <TabsContent value="swarm" className="pt-4">
                <SwarmTab settings={settings} onChange={setSettings} />
              </TabsContent>

              <TabsContent
                value="tools"
                className="thin-scrollbar max-h-96 overflow-y-auto pt-4"
              >
                <ToolsTab
                  settings={settings}
                  catalog={catalog}
                  onChange={setSettings}
                />
              </TabsContent>

              <TabsContent value="general" className="pt-4">
                <GeneralTab
                  theme={theme}
                  onThemeChange={onThemeChange}
                  meta={meta}
                  settings={settings}
                  onChange={setSettings}
                />
              </TabsContent>
            </Tabs>
          )}

          {error ? <p className="text-sm text-destructive">{error}</p> : null}

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={!settings || saving}>
              {saving ? <Loader2 className="animate-spin" /> : null}
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function ModelsTab({
  settings,
  onChange,
}: {
  settings: Settings
  onChange: (s: Settings) => void
}) {
  const update = (
    index: number,
    patch: Partial<Settings["models"]["providers"][0]>,
  ) => {
    const providers = settings.models.providers.map((p, i) =>
      i === index ? { ...p, ...patch } : p,
    )
    onChange({ ...settings, models: { ...settings.models, providers } })
  }

  return (
    <div className="space-y-4">
      {settings.models.providers.map((p, i) => (
        <div
          key={p.id}
          className="space-y-3 rounded-lg border border-border p-3"
        >
          <div className="flex items-center gap-2">
            <Input
              value={p.label}
              placeholder="Name this endpoint"
              className="h-8 flex-1"
              onChange={(e) => update(i, { label: e.target.value })}
            />
            {settings.models.default === p.id ? (
              <Badge variant="success">default</Badge>
            ) : (
              <Button
                variant="ghost"
                size="sm"
                onClick={() =>
                  onChange({
                    ...settings,
                    models: { ...settings.models, default: p.id },
                  })
                }
              >
                Make default
              </Button>
            )}
            {settings.models.providers.length > 1 ? (
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Remove endpoint"
                onClick={() => {
                  const providers = settings.models.providers.filter(
                    (_, j) => j !== i,
                  )
                  onChange({
                    ...settings,
                    models: {
                      default:
                        settings.models.default === p.id
                          ? providers[0].id
                          : settings.models.default,
                      providers,
                    },
                  })
                }}
              >
                <Trash2 />
              </Button>
            ) : null}
          </div>

          <Field label="Base URL">
            <Input
              value={p.base_url}
              placeholder="https://your-endpoint/v1"
              onChange={(e) => update(i, { base_url: e.target.value })}
            />
          </Field>
          <Field label="Model">
            <Input
              value={p.model}
              placeholder="the model name this endpoint serves"
              onChange={(e) => update(i, { model: e.target.value })}
            />
          </Field>
          <Field
            label="API key"
            hint={
              p.has_api_key && p.api_key === undefined
                ? "A key is stored. Type to replace it."
                : "Leave empty for endpoints that need no key."
            }
          >
            <Input
              type="password"
              // An API key is not the user's password; offering to fill it
              // from a password manager only gets the wrong value in.
              autoComplete="off"
              value={p.api_key ?? ""}
              placeholder={p.has_api_key ? "••••••••" : ""}
              onChange={(e) => update(i, { api_key: e.target.value })}
            />
          </Field>
          <Field label="Request timeout (seconds)">
            <Input
              type="number"
              min={10}
              value={p.timeout_seconds}
              onChange={(e) =>
                update(i, { timeout_seconds: Number(e.target.value) })
              }
            />
          </Field>
        </div>
      ))}

      <Button
        variant="outline"
        size="sm"
        className="gap-1.5"
        onClick={() => {
          const id = `provider-${settings.models.providers.length + 1}`
          onChange({
            ...settings,
            models: {
              ...settings.models,
              providers: [
                ...settings.models.providers,
                {
                  id,
                  label: "",
                  base_url: "",
                  model: "",
                  timeout_seconds: 300,
                  has_api_key: false,
                  ready: false,
                },
              ],
            },
          })
        }}
      >
        <Plus />
        Add an endpoint
      </Button>
    </div>
  )
}

function SwarmTab({
  settings,
  onChange,
}: {
  settings: Settings
  onChange: (s: Settings) => void
}) {
  const update = (patch: Partial<Settings["swarm"]>) =>
    onChange({ ...settings, swarm: { ...settings.swarm, ...patch } })
  return (
    <div className="space-y-3">
      <Field
        label="Sub-agents at once"
        hint="More means faster fan-out and more tokens burned in parallel."
      >
        <Input
          type="number"
          min={1}
          value={settings.swarm.max_concurrent}
          onChange={(e) => update({ max_concurrent: Number(e.target.value) })}
        />
      </Field>
      <Field
        label="Sub-agent timeout (seconds)"
        hint="How long one sub-agent may keep working before it is stopped."
      >
        <Input
          type="number"
          min={30}
          value={settings.swarm.agent_timeout_seconds}
          onChange={(e) =>
            update({ agent_timeout_seconds: Number(e.target.value) })
          }
        />
      </Field>
      <Field
        label="Sub-agent tool rounds"
        hint="How many times a sub-agent may think and call a tool before it stops."
      >
        <Input
          type="number"
          min={1}
          value={settings.swarm.max_turns}
          onChange={(e) => update({ max_turns: Number(e.target.value) })}
        />
      </Field>
      <Field
        label="Manager tool rounds"
        hint="Spawning and waiting for sub-agents spends the manager's rounds too."
      >
        <Input
          type="number"
          min={1}
          value={settings.swarm.manager_max_iterations}
          onChange={(e) =>
            update({ manager_max_iterations: Number(e.target.value) })
          }
        />
      </Field>
      <Field
        label="Progress pulse (seconds)"
        hint="How often a running turn reports in while nothing is streaming."
      >
        <Input
          type="number"
          min={1}
          value={settings.swarm.progress_interval_seconds}
          onChange={(e) =>
            update({ progress_interval_seconds: Number(e.target.value) })
          }
        />
      </Field>
    </div>
  )
}

function ToolsTab({
  settings,
  catalog,
  onChange,
}: {
  settings: Settings
  catalog: ToolDescriptor[]
  onChange: (s: Settings) => void
}) {
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
    <div className="space-y-4">
      {groups.map((group) => (
        <div key={group}>
          <p className="px-1 pb-1 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
            {group}
          </p>
          <div className="space-y-1">
            {catalog
              .filter((t) => t.group === group)
              .map((t) => (
                <label
                  key={t.name}
                  className="flex cursor-pointer items-start gap-3 rounded-md px-2 py-1.5 hover:bg-accent/60"
                >
                  <Switch
                    checked={isOn(t)}
                    onCheckedChange={(on) => toggle(t, on)}
                    className="mt-0.5"
                  />
                  <div className="min-w-0">
                    <p className="text-sm">
                      <span className="font-mono text-[13px]">{t.name}</span>
                      <span className="text-muted-foreground">
                        {" "}
                        — {t.title}
                      </span>
                    </p>
                    <p className="text-xs text-muted-foreground">{t.summary}</p>
                  </div>
                </label>
              ))}
          </div>
        </div>
      ))}

      <div className="space-y-3 rounded-lg border border-border p-3">
        <p className="text-sm font-medium">
          Proxy for tools that reach the network
        </p>
        <Field label="HTTP">
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
        <Field label="HTTPS">
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
        <Field label="Skip the proxy for" hint="Comma-separated hosts.">
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
      </div>
    </div>
  )
}

function GeneralTab({
  theme,
  onThemeChange,
  meta,
  settings,
  onChange,
}: {
  theme: Theme
  onThemeChange: (t: Theme) => void
  meta?: Meta
  settings: Settings
  onChange: (s: Settings) => void
}) {
  return (
    <div className="space-y-4">
      {/* Radix renders a button rather than a <select>, so these are labelled
          on the trigger itself instead of through Field's generated id. */}
      <div className="space-y-1.5">
        <Label>Appearance</Label>
        <Select value={theme} onValueChange={(v) => onThemeChange(v as Theme)}>
          <SelectTrigger className="h-9 text-sm" aria-label="Appearance">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="system">Match the system</SelectItem>
            <SelectItem value="light">Light</SelectItem>
            <SelectItem value="dark">Dark</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="space-y-1.5">
        <Label>Log level</Label>
        <Select
          value={settings.log.level}
          onValueChange={(level) => onChange({ ...settings, log: { level } })}
        >
          <SelectTrigger className="h-9 text-sm" aria-label="Log level">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {["debug", "info", "warn", "error"].map((l) => (
              <SelectItem key={l} value={l}>
                {l}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="rounded-lg border border-border p-3 text-xs text-muted-foreground">
        <p>
          Data directory: <code className="font-mono">{meta?.data_dir}</code>
        </p>
        <p className="mt-1">
          Version {meta?.version} · {meta?.mode} mode
          {meta?.mock ? " · offline scripted provider" : ""}
        </p>
      </div>
    </div>
  )
}

/** A labelled control. The id is generated and handed to the child so the
 *  label actually points at its input — clicking the text focuses the field,
 *  and a screen reader (or a test) can find it by name. */
function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactElement<{ id?: string }>
}) {
  const id = useId()
  return (
    <div className="space-y-1.5">
      <Label htmlFor={id}>{label}</Label>
      {cloneElement(children, { id })}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}
