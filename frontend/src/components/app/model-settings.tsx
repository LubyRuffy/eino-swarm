import { ChevronRight, Loader2, Plus, RefreshCw, Trash2 } from "lucide-react"
import { useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api } from "@/lib/api"
import { mergeModelContext } from "@/lib/usage"
import type { Settings } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT, type Translate } from "@/lib/use-t"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

import { AuxiliaryModels } from "./auxiliary-models"
import { SearchSettingsPanel } from "./settings-search"
import { ConfirmDeleteDialog } from "./confirm-delete-dialog"
import { ProviderWindows } from "./provider-windows"
import {
  Field,
  SettingsPage,
  SettingsRow,
  SettingsSection,
  settingsMatch,
  settingsSelectTriggerClass,
} from "./settings-field"

type ProviderConfig = Settings["models"]["providers"][number]

export function ModelsTab({
  settings,
  onChange,
  query = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
}) {
  const t = useT()
  const [busy, setBusy] = useState<string>()
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set())
  const [doomed, setDoomed] = useState<ProviderConfig>()

  const update = (
    index: number,
    patch: Partial<ProviderConfig>,
  ) => {
    const providers = settings.models.providers.map((p, i) =>
      i === index ? { ...p, ...patch } : p,
    )
    onChange({ ...settings, models: { ...settings.models, providers } })
  }

  const discover = async (index: number) => {
    const p = settings.models.providers[index]
    const toastId = `discover:${p.id}`
    setBusy(p.id)
    useToasts.getState().dismiss(toastId)
    try {
      const listed = await api.discoverModels({
        provider_id: p.id,
        base_url: p.base_url,
        api_key: p.api_key,
      })
      const catalog = listed.models
      const nextModel = p.model || catalog[0] || ""
      update(index, {
        catalog,
        model: nextModel,
        model_context: mergeModelContext(p.model_context, listed.context_windows),
      })
    } catch (e) {
      // The page is scrolled to this provider. A red line under the Models
      // heading is above the fold and looks like nothing happened.
      toastError(errorMessage(e), {
        id: toastId,
        title: t("settings.models.discoverFailed"),
      })
    } finally {
      setBusy(undefined)
    }
  }

  const toggle = (id: string) => {
    setOpen((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const removeProvider = (target: ProviderConfig) => {
    const providers = settings.models.providers.filter((p) => p.id !== target.id)
    if (providers.length === 0) return
    setOpen((prev) => {
      const next = new Set(prev)
      next.delete(target.id)
      return next
    })
    onChange({
      ...settings,
      models: {
        default:
          settings.models.default === target.id
            ? providers[0].id
            : settings.models.default,
        providers,
      },
    })
  }

  return (
    <SettingsPage
      title={t("settings.models.title")}
      description={t("settings.models.desc")}
    >
      <AuxiliaryModels settings={settings} onChange={onChange} query={query} />
      <SearchSettingsPanel settings={settings} onChange={onChange} query={query} />

      <SettingsSection
        title={t("settings.models.providers")}
        description={t("settings.models.providersDesc")}
      >
        {settings.models.providers.map((p, i) => (
          <ProviderRow
            key={p.id}
            provider={p}
            query={query}
            isDefault={settings.models.default === p.id}
            canRemove={settings.models.providers.length > 1}
            open={open.has(p.id) || providerFieldsMatch(query, p, t)}
            busy={busy === p.id}
            onToggle={() => toggle(p.id)}
            onUpdate={(patch) => update(i, patch)}
            onDiscover={() => void discover(i)}
            onMakeDefault={() =>
              onChange({
                ...settings,
                models: { ...settings.models, default: p.id },
              })
            }
            onRemove={() => setDoomed(p)}
          />
        ))}
      </SettingsSection>

      {settingsMatch(query, t("settings.models.add"), "provider") ? (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="gap-1.5 self-start"
          onClick={() => {
            const id = `provider-${settings.models.providers.length + 1}`
            setOpen(new Set([id]))
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
                    catalog: [],
                    timeout_seconds: 300,
                    context_window: 0,
                    model_context: {},
                    has_api_key: false,
                    ready: false,
                  },
                ],
              },
            })
          }}
        >
          <Plus />
          {t("settings.models.add")}
        </Button>
      ) : null}
      <ConfirmDeleteDialog
        open={Boolean(doomed)}
        title={t("provider.deleteTitle", {
          name: doomed ? providerHeading(doomed) : "",
        })}
        description={t("provider.deleteDesc")}
        confirmLabel={t("provider.deleteConfirm")}
        cancelLabel={t("confirm.cancel")}
        onOpenChange={(open) => {
          if (!open) setDoomed(undefined)
        }}
        onConfirm={() => {
          if (doomed) removeProvider(doomed)
        }}
      />
    </SettingsPage>
  )
}

function ProviderRow({
  provider: p,
  query,
  isDefault,
  canRemove,
  open,
  busy,
  onToggle,
  onUpdate,
  onDiscover,
  onMakeDefault,
  onRemove,
}: {
  provider: ProviderConfig
  query: string
  isDefault: boolean
  canRemove: boolean
  open: boolean
  busy: boolean
  onToggle: () => void
  onUpdate: (patch: Partial<ProviderConfig>) => void
  onDiscover: () => void
  onMakeDefault: () => void
  onRemove: () => void
}) {
  const t = useT()
  const detailsId = `provider-details-${p.id}`
  if (!providerVisible(query, p, t)) return null

  const heading = providerHeading(p)
  const subtitle = p.model.trim() || p.base_url.trim() || t("settings.models.noModel")
  const options = unique([...(p.catalog ?? []), p.model])

  return (
    <div>
      <div
        className="flex items-start gap-2 px-4 py-3 hover:bg-accent/60"
        data-settings-row=""
      >
        <button
          type="button"
          aria-expanded={open}
          aria-controls={detailsId}
          aria-label={t("settings.models.details", { name: heading })}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onClick={onToggle}
        >
          <ChevronRight
            aria-hidden
            className={cn(
              "size-3.5 shrink-0 text-muted-foreground transition-transform",
              open && "rotate-90",
            )}
          />
          <span className="min-w-0 flex-1">
            <span className="block truncate text-[13px] font-normal text-foreground/90">
              {heading}
            </span>
            <span className="mt-0.5 block truncate text-[13px] font-normal text-muted-foreground">
              {subtitle}
            </span>
          </span>
        </button>
        <div className="flex shrink-0 items-center gap-1 pt-0.5">
          {isDefault ? (
            <Badge variant="success">{t("settings.models.default")}</Badge>
          ) : (
            <Button type="button" variant="ghost" size="sm" onClick={onMakeDefault}>
              {t("settings.models.makeDefault")}
            </Button>
          )}
          {canRemove ? (
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label={t("settings.models.remove", { name: heading })}
              onClick={onRemove}
            >
              <Trash2 />
            </Button>
          ) : null}
        </div>
      </div>
      {open ? (
        <div id={detailsId} className="divide-y divide-border border-t border-border">
          <Field query={query} label={t("settings.models.provider")}>
            <Input
              value={p.label}
              placeholder={t("settings.models.providerPlaceholder")}
              aria-label={t("settings.models.provider")}
              onChange={(e) => onUpdate({ label: e.target.value })}
            />
          </Field>
          <Field query={query} label={t("settings.models.baseUrl")} wide>
            <Input
              value={p.base_url}
              placeholder="https://your-endpoint/v1"
              onChange={(e) => onUpdate({ base_url: e.target.value })}
            />
          </Field>
          <Field
            query={query}
            label={t("settings.models.apiKey")}
            hint={
              p.has_api_key && p.api_key === undefined
                ? t("settings.models.apiKeySet")
                : t("settings.models.apiKeyHint")
            }
          >
            <Input
              type="password"
              autoComplete="off"
              value={p.api_key ?? ""}
              placeholder={p.has_api_key ? "••••••••" : ""}
              onChange={(e) => onUpdate({ api_key: e.target.value })}
            />
          </Field>
          {settingsMatch(query, t("settings.models.defaultModel"), t("settings.models.discover")) ? (
            <SettingsRow
              label={t("settings.models.defaultModel")}
              hint={
                options.length > 0
                  ? t("settings.models.defaultModelHint", {
                      n: options.length,
                      s: options.length === 1 ? "" : "s",
                    })
                  : t("settings.models.defaultModelEmpty")
              }
            >
              <div className="flex flex-col items-end gap-1.5">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-7 justify-end gap-1.5"
                  disabled={!p.base_url.trim() || busy}
                  onClick={onDiscover}
                >
                  {busy ? (
                    <Loader2 className="size-3.5 animate-spin" />
                  ) : (
                    <RefreshCw className="size-3.5" />
                  )}
                  {t("settings.models.discover")}
                </Button>
                {options.length > 0 ? (
                  <Select
                    value={p.model || options[0]}
                    onValueChange={(model) => onUpdate({ model })}
                  >
                    <SelectTrigger
                      className={settingsSelectTriggerClass}
                      aria-label={t("settings.models.defaultModel")}
                    >
                      <SelectValue placeholder={t("settings.models.pickDefault")} />
                    </SelectTrigger>
                    <SelectContent>
                      {options.map((name) => (
                        <SelectItem key={name} value={name}>
                          {name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                ) : (
                  <Input
                    value={p.model}
                    placeholder={t("settings.models.typeModel")}
                    aria-label={t("settings.models.defaultModel")}
                    className="h-[32px] w-56 text-[13px] shadow-none"
                    onChange={(e) => onUpdate({ model: e.target.value })}
                  />
                )}
              </div>
            </SettingsRow>
          ) : null}
          <ProviderWindows
            query={query}
            names={options}
            provider={p}
            onUpdate={onUpdate}
          />
          <Field query={query} label={t("settings.models.timeout")} hint={t("settings.models.timeoutHint")}>
            <Input
              type="number"
              min={10}
              value={p.timeout_seconds}
              onChange={(e) => onUpdate({ timeout_seconds: Number(e.target.value) })}
            />
          </Field>
        </div>
      ) : null}
    </div>
  )
}

function providerHeading(p: ProviderConfig): string {
  return p.label.trim() || p.id
}

function providerVisible(query: string, p: ProviderConfig, t: Translate): boolean {
  return settingsMatch(
    query,
    providerHeading(p),
    p.id,
    p.model,
    p.base_url,
    "provider",
    t("settings.models.provider"),
    t("settings.models.baseUrl"),
    t("settings.models.apiKey"),
    t("settings.models.defaultModel"),
    t("settings.models.discover"),
    t("settings.models.window"),
    t("settings.models.windows"),
    t("settings.models.windowFallback"),
    t("settings.models.timeout"),
    t("settings.models.timeoutHint"),
    ...(p.catalog ?? []),
  )
}

function providerFieldsMatch(query: string, p: ProviderConfig, t: Translate): boolean {
  if (!query.trim()) return false
  return settingsMatch(
    query,
    t("settings.models.provider"),
    t("settings.models.baseUrl"),
    t("settings.models.apiKey"),
    t("settings.models.defaultModel"),
    t("settings.models.discover"),
    t("settings.models.window"),
    t("settings.models.windows"),
    t("settings.models.windowFallback"),
    t("settings.models.timeout"),
    t("settings.models.timeoutHint"),
    p.base_url,
    p.model,
  )
}

function unique(names: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const n of names) {
    const s = n.trim()
    if (!s || seen.has(s)) continue
    seen.add(s)
    out.push(s)
  }
  return out
}
