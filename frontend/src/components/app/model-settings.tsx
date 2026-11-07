import { ChevronRight, Loader2, Plus, RefreshCw, Trash2 } from "lucide-react"
import { useEffect, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { api } from "@/lib/api"
import { accountModelRows, effectiveAccountModel, serverStartsConversations } from "@/lib/account-models"
import { chromeTypeClass } from "@/lib/chrome-type"
import { firstByteTimeoutSettingsKey } from "@/lib/first-byte-timeout"
import { formatTokens, mergeModelContext } from "@/lib/usage"
import type { ModelInfo, Settings } from "@/lib/types"
import type { MessageKey } from "@/lib/i18n"
import type { ProviderToolsDrafts } from "@/lib/provider-tools"
import { cn } from "@/lib/utils"
import { useT, type Translate } from "@/lib/use-t"
import { useApp } from "@/store/app"
import { errorMessage, toastError, useToasts } from "@/store/toasts"

import { AccountModelsCard } from "./account-models-card"
import { AuxiliaryModels } from "./auxiliary-models"
import { SearchSettingsPanel } from "./settings-search"
import { ConfirmDeleteDialog } from "./confirm-delete-dialog"
import { ProviderWindows } from "./provider-windows"
import { ProviderCloudTools } from "./provider-cloud-tools"
import {
  Field,
  SettingsPage,
  SettingsRow,
  SettingsSection,
  settingsHintClass,
  settingsLabelClass,
  settingsMatch,
  settingsSelectTriggerClass,
} from "./settings-field"

type ProviderConfig = Settings["models"]["providers"][number]

export function ModelsTab({
  settings,
  onChange,
  query = "",
  toolsDrafts = {},
  toolsErrors = {},
  onToolsDraftChange,
  focusKey = "",
}: {
  settings: Settings
  onChange: (s: Settings) => void
  query?: string
  toolsDrafts?: ProviderToolsDrafts
  toolsErrors?: Record<string, MessageKey>
  onToolsDraftChange?: (id: string, text: string) => void
  focusKey?: string
}) {
  const t = useT()
  const account = useApp((s) => s.meta?.account_models)
  const listed = useApp((s) => s.models)
  const accountReady = account?.state === "ready" && account.models > 0
  const serverStarts = serverStartsConversations(settings.models.default_source, accountReady)
  const [busy, setBusy] = useState<string>()
  const [open, setOpen] = useState<ReadonlySet<string>>(() => new Set([
    ...Object.keys(toolsErrors),
    ...settings.models.providers
      .filter((p) => focusKey === firstByteTimeoutSettingsKey(p.id))
      .map((p) => p.id),
  ]))
  const [doomed, setDoomed] = useState<ProviderConfig>()

  useEffect(() => {
    setOpen((previous) => {
      const missing = Object.keys(toolsErrors).filter((id) => !previous.has(id))
      return missing.length ? new Set([...previous, ...missing]) : previous
    })
  }, [toolsErrors])

  const update = (
    index: number,
    patch: Partial<ProviderConfig>,
  ) => {
    const providers = settings.models.providers.map((p, i) =>
      i === index ? { ...p, ...patch } : p,
    )
    let nextDefault = settings.models.default
    const turnedOff = patch.enabled === false && providers[index]?.id === nextDefault
    if (turnedOff) {
      const fallback = providers.find((row) => row.enabled !== false)
      if (fallback) nextDefault = fallback.id
    }
    onChange({
      ...settings,
      models: { ...settings.models, default: nextDefault, providers },
    })
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
        ...settings.models,
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
      <AccountModelsCard settings={settings} onChange={onChange} query={query} />
      <AuxiliaryModels settings={settings} onChange={onChange} query={query} />
      <SearchSettingsPanel settings={settings} onChange={onChange} query={query} />

      <SettingsSection
        title={t("accountModels.ownTitle")}
        description={noOwnProvider(settings) ? t("accountModels.ownEmpty") : t("accountModels.ownDesc")}
      >
        {accountReady ? (
          <ServerDefaultRow
            query={query}
            models={listed}
            accountModel={settings.models.account_model}
            isDefault={serverStarts}
            onMakeDefault={() =>
              onChange({
                ...settings,
                models: { ...settings.models, default_source: "account" },
              })
            }
            onModel={(model) =>
              onChange({
                ...settings,
                models: { ...settings.models, account_model: model },
              })
            }
          />
        ) : null}
        {settings.models.providers.map((p, i) => (
          <ProviderRow
            key={p.id}
            provider={p}
            query={query}
            isDefault={settings.models.default === p.id && !serverStarts}
            canRemove={settings.models.providers.length > 1}
            lockOn={p.enabled !== false && listedCount(settings) < 2}
            open={open.has(p.id) || Boolean(toolsErrors[p.id]) || providerFieldsMatch(query, p, t)}
            busy={busy === p.id}
            onToggle={() => toggle(p.id)}
            onUpdate={(patch) => update(i, patch)}
            onDiscover={() => void discover(i)}
            onMakeDefault={() =>
              onChange({
                ...settings,
                models: { ...settings.models, default: p.id, default_source: "provider" },
              })
            }
            onRemove={() => setDoomed(p)}
            toolsDraft={toolsDrafts[p.id]}
            toolsError={toolsErrors[p.id]}
            onToolsDraftChange={(text) => onToolsDraftChange?.(p.id, text)}
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
                    api_mode: "chat_completions",
                    builtin_tools: [],
                    model: "",
                    catalog: [],
                    timeout_seconds: 300,
                    context_window: 0,
                    model_context: {},
                    has_api_key: false,
                    ready: false,
                    enabled: true,
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

/** Sits in the own-provider list because that is where the default badge is.
 *  The source menu on the official card is above other settings, so a person
 *  looking at their own service never saw the server as a choice. */
function ServerDefaultRow({
  query,
  models,
  accountModel,
  isDefault,
  onMakeDefault,
  onModel,
}: {
  query: string
  models: ModelInfo[]
  accountModel?: string
  isDefault: boolean
  onMakeDefault: () => void
  onModel: (model: string) => void
}) {
  const t = useT()
  const rows = accountModelRows(models)
  const current = effectiveAccountModel(models, accountModel)
  const [open, setOpen] = useState(false)
  const shown = open || (query.trim() !== "" && rows.some((row) => settingsMatch(query, row.model)))
  if (!settingsMatch(
    query,
    t("accountModels.defaultAccount"),
    t("accountModels.defaultRowHint"),
    t("accountModels.defaultModel"),
    t("accountModels.defaultTitle"),
    current,
    ...rows.map((row) => row.model),
    "server",
  )) return null
  return (
    <div data-testid="server-default-row">
      <div className="flex items-center gap-2 px-4 py-3" data-settings-row="">
        <button
          type="button"
          aria-expanded={shown}
          aria-label={t("accountModels.serverDetails")}
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md text-left focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
          onClick={() => setOpen((value) => !value)}
        >
          <ChevronRight
            aria-hidden
            className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", shown && "rotate-90")}
          />
          <span className="min-w-0 flex-1">
            <span className={cn("block truncate", settingsLabelClass)}>
              {t("accountModels.defaultAccount")}
            </span>
            <span className={cn("block truncate", settingsHintClass)}>
              {current || t("accountModels.defaultRowHint")}
            </span>
          </span>
        </button>
        {isDefault ? (
          <Badge variant="success">{t("settings.models.default")}</Badge>
        ) : (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            aria-label={t("accountModels.makeServerDefault")}
            onClick={onMakeDefault}
          >
            {t("settings.models.makeDefault")}
          </Button>
        )}
      </div>
      {shown && rows.length > 0 ? (
        <div className="border-t border-border px-4 py-3">
          <p className={settingsLabelClass}>{t("accountModels.defaultModel")}</p>
          <ul aria-label={t("accountModels.listLabel")} className="mt-2 flex flex-wrap gap-1.5">
            {rows.map((row) => {
              const selected = row.model === current
              return (
                <li key={row.id || row.model}>
                  <button
                    type="button"
                    aria-pressed={selected}
                    aria-label={row.model}
                    className="rounded-md focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
                    onClick={() => onModel(row.model)}
                  >
                    <Badge variant={selected ? "success" : "outline"}>
                      <span>{row.label || row.model}</span>
                      {row.context_window ? (
                        <span className="font-normal opacity-70">{formatTokens(row.context_window)}</span>
                      ) : null}
                    </Badge>
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
      ) : null}
    </div>
  )
}

function ProviderRow({
  provider: p,
  query,
  isDefault,
  canRemove,
  lockOn,
  open,
  busy,
  onToggle,
  onUpdate,
  onDiscover,
  onMakeDefault,
  onRemove,
  toolsDraft,
  toolsError,
  onToolsDraftChange,
}: {
  provider: ProviderConfig
  query: string
  isDefault: boolean
  canRemove: boolean
  /** The composer needs one endpoint. This row is that one. */
  lockOn: boolean
  open: boolean
  busy: boolean
  onToggle: () => void
  onUpdate: (patch: Partial<ProviderConfig>) => void
  onDiscover: () => void
  onMakeDefault: () => void
  onRemove: () => void
  toolsDraft?: string
  toolsError?: MessageKey
  onToolsDraftChange: (text: string) => void
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
          className="flex min-w-0 flex-1 items-center gap-2 rounded-md text-left focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
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
            <span
              className={cn(
                "block truncate",
                settingsLabelClass,
                p.enabled === false && "text-muted-foreground",
              )}
            >
              {heading}
            </span>
            <span className={cn("block truncate", settingsHintClass)}>
              {subtitle}
            </span>
          </span>
        </button>
        <div className="flex shrink-0 items-center gap-1 pt-0.5">
          <Switch
            checked={p.enabled !== false}
            disabled={lockOn}
            aria-label={t("settings.models.listed", { name: heading })}
            title={t("settings.models.listedHint")}
            onCheckedChange={(enabled) => onUpdate({ enabled })}
          />
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
          <ProviderCloudTools
            provider={p}
            query={query}
            draft={toolsDraft}
            error={toolsError}
            onUpdate={onUpdate}
            onDraftChange={onToolsDraftChange}
          />
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
                    className={cn("h-[32px] w-56 shadow-none", chromeTypeClass)}
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
          <Field
            query={query}
            label={t("settings.models.firstByteTimeout")}
            hint={t("settings.models.firstByteTimeoutHint")}
            anchor={firstByteTimeoutSettingsKey(p.id)}
          >
            <Input
              type="number"
              min={0}
              value={p.first_byte_timeout_seconds ?? 0}
              onChange={(e) => onUpdate({ first_byte_timeout_seconds: Number(e.target.value) })}
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
    t("settings.models.apiMode"),
    t("settings.models.apiModeHint"),
    t("settings.models.builtinTools"),
    t("settings.models.builtinToolsHint"),
    t("settings.models.defaultModel"),
    t("settings.models.discover"),
    t("settings.models.window"),
    t("settings.models.windows"),
    t("settings.models.windowFallback"),
    t("settings.models.timeout"),
    t("settings.models.timeoutHint"),
    t("settings.models.firstByteTimeout"),
    t("settings.models.firstByteTimeoutHint"),
    t("settings.models.listed", { name: providerHeading(p) }),
    t("settings.models.listedHint"),
    ...(p.catalog ?? []),
  )
}

function listedCount(settings: Settings): number {
  return settings.models.providers.filter((row) => row.enabled !== false).length
}

function providerFieldsMatch(query: string, p: ProviderConfig, t: Translate): boolean {
  if (!query.trim()) return false
  return settingsMatch(
    query,
    t("settings.models.provider"),
    t("settings.models.baseUrl"),
    t("settings.models.apiKey"),
    t("settings.models.apiMode"),
    t("settings.models.apiModeHint"),
    t("settings.models.builtinTools"),
    t("settings.models.builtinToolsHint"),
    t("settings.models.defaultModel"),
    t("settings.models.discover"),
    t("settings.models.window"),
    t("settings.models.windows"),
    t("settings.models.windowFallback"),
    t("settings.models.timeout"),
    t("settings.models.timeoutHint"),
    t("settings.models.firstByteTimeout"),
    t("settings.models.firstByteTimeoutHint"),
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

/** Only the untouched placeholder row exists: show the empty state, not a
 *  form that looks like a configured service. */
function noOwnProvider(settings: Settings): boolean {
  return settings.models.providers.every((p) => !p.ready)
}
