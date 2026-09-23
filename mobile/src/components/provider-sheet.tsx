import { Loader2, X } from "lucide-react"
import { useState, type ReactNode } from "react"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  blankProvider,
  type DirectProvider,
  saveProviders,
} from "@/lib/direct-provider"
import { t } from "@/lib/i18n"
import { discoverModels } from "@/lib/openai-client"
import { httpBaseURL, ModelCallError, normalizeApiStyle, redact } from "@/lib/openai-wire"

const levels = ["chat", "responses"] as const

/** The same fields as desktop Settings → Models, plus which OpenAI-compatible
 *  wire this phone should call. The key stays on the device. */
export function ProviderSheet({
  providers,
  onClose,
  onChange,
  discover = discoverModels,
}: {
  providers: DirectProvider[]
  onClose: () => void
  onChange: (next: DirectProvider[]) => void
  discover?: typeof discoverModels
}) {
  const [editing, setEditing] = useState<DirectProvider | null>(
    providers.length === 0 ? blankProvider() : null,
  )
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [confirming, setConfirming] = useState(false)

  const commit = (next: DirectProvider[]) => {
    onChange(saveProviders(next))
  }

  const save = () => {
    if (!editing) return
    if (!httpBaseURL(editing.baseURL)) {
      setError(t("chat.badUrl"))
      return
    }
    const nextRow = {
      ...editing,
      api: normalizeApiStyle(editing.api),
      baseURL: httpBaseURL(editing.baseURL) ?? editing.baseURL,
    }
    const rest = providers.filter((row) => row.id !== nextRow.id)
    commit([...rest, nextRow])
    onClose()
  }

  const remove = () => {
    if (!editing) return
    if (!confirming) {
      setConfirming(true)
      return
    }
    const next = providers.filter((row) => row.id !== editing.id)
    commit(next)
    onClose()
  }

  const runDiscover = async () => {
    if (!editing) return
    const current = editing
    setError("")
    setBusy(true)
    try {
      const names = await discover(current)
      setEditing((prev) => {
        if (!prev || prev.id !== current.id) return prev
        return {
          ...prev,
          catalog: names,
          model: prev.model.trim() || names[0] || "",
        }
      })
    } catch (err) {
      const detail =
        err instanceof ModelCallError && err.message && err.message !== "empty"
          ? redact(err.message, current.apiKey)
          : ""
      setError(detail && detail !== "bad-url" ? detail : t("chat.discoverFailed"))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex flex-col justify-end">
      <button
        type="button"
        className="absolute inset-0 bg-foreground/40"
        aria-label={t("home.close")}
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-labelledby="provider-sheet-title"
        data-testid="provider-sheet"
        className="relative z-10 flex max-h-[85%] flex-col overflow-hidden rounded-t-2xl border border-border bg-background pb-[env(safe-area-inset-bottom)]"
      >
        <div className="flex items-center justify-between px-5 pb-1 pt-3">
          <h2 id="provider-sheet-title" className="text-lg font-semibold tracking-tight">
            {editing ? (providers.some((row) => row.id === editing.id) ? t("chat.edit") : t("chat.add")) : t("chat.models")}
          </h2>
          <Button
            type="button"
            variant="ghost"
            className="size-10 shrink-0 px-0"
            aria-label={t("home.close")}
            onClick={onClose}
          >
            <X className="size-5" aria-hidden />
          </Button>
        </div>
        {editing ? (
          <form
            className="flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-5 pb-5"
            onSubmit={(e) => {
              e.preventDefault()
              save()
            }}
          >
            <Field label={t("chat.name")}>
              <Input
                aria-label={t("chat.name")}
                value={editing.label}
                onChange={(e) => setEditing({ ...editing, label: e.target.value })}
              />
            </Field>
            <Field label={t("chat.baseUrl")}>
              <Input
                aria-label={t("chat.baseUrl")}
                value={editing.baseURL}
                placeholder={t("chat.baseUrlPlaceholder")}
                autoCapitalize="off"
                autoCorrect="off"
                spellCheck={false}
                onChange={(e) => setEditing({ ...editing, baseURL: e.target.value })}
              />
            </Field>
            <Field label={t("chat.apiKey")} hint={t("chat.apiKeyHint")}>
              <Input
                aria-label={t("chat.apiKey")}
                type="password"
                autoComplete="off"
                value={editing.apiKey}
                onChange={(e) => setEditing({ ...editing, apiKey: e.target.value })}
              />
            </Field>
            <Field label={t("chat.api")}>
              <select
                aria-label={t("chat.api")}
                className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
                value={editing.api}
                onChange={(e) =>
                  setEditing({ ...editing, api: normalizeApiStyle(e.target.value) })
                }
              >
                {levels.map((level) => (
                  <option key={level} value={level}>
                    {level === "responses" ? t("chat.apiResponses") : t("chat.apiChat")}
                  </option>
                ))}
              </select>
            </Field>
            <Field label={t("chat.defaultModel")}>
              <div className="flex flex-col gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={busy || !editing.baseURL.trim()}
                  onClick={() => void runDiscover()}
                >
                  {busy ? <Loader2 className="size-4 motion-safe:animate-spin" aria-hidden /> : null}
                  {busy ? t("chat.discovering") : t("chat.discover")}
                </Button>
                {editing.catalog.length > 0 ? (
                  <select
                    aria-label={t("chat.defaultModel")}
                    className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm"
                    value={editing.model || editing.catalog[0]}
                    onChange={(e) => setEditing({ ...editing, model: e.target.value })}
                  >
                    {unique([editing.model, ...editing.catalog]).map((name) => (
                      <option key={name} value={name}>
                        {name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <Input
                    aria-label={t("chat.defaultModel")}
                    value={editing.model}
                    placeholder={t("chat.typeModel")}
                    onChange={(e) => setEditing({ ...editing, model: e.target.value })}
                  />
                )}
              </div>
            </Field>
            <Field label={t("chat.timeout")}>
              <Input
                aria-label={t("chat.timeout")}
                type="number"
                min={10}
                value={editing.timeoutSeconds}
                onChange={(e) =>
                  setEditing({ ...editing, timeoutSeconds: Number(e.target.value) })
                }
              />
            </Field>
            {error ? (
              <p className="text-sm text-destructive" role="alert">
                {error}
              </p>
            ) : null}
            <div className="flex gap-2">
              <Button type="submit" className="flex-1">
                {t("chat.save")}
              </Button>
              {providers.some((row) => row.id === editing.id) ? (
                <Button type="button" variant="outline" onClick={remove}>
                  {confirming ? t("chat.removeConfirm") : t("chat.remove")}
                </Button>
              ) : null}
            </div>
            {providers.length > 0 ? (
              <Button type="button" variant="ghost" onClick={() => setEditing(null)}>
                {t("chat.models")}
              </Button>
            ) : null}
          </form>
        ) : (
          <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-y-auto px-5 pb-5">
            <ul className="flex flex-col gap-2">
              {providers.map((row) => (
                <li key={row.id}>
                  <button
                    type="button"
                    className="flex w-full flex-col rounded-xl bg-muted px-3 py-2 text-left"
                    onClick={() => {
                      setError("")
                      setConfirming(false)
                      setEditing(row)
                    }}
                  >
                    <span className="truncate text-sm font-medium">{row.label || row.id}</span>
                    <span className="truncate text-xs text-muted-foreground">
                      {(row.model || row.baseURL) + " · " + (row.api === "responses" ? t("chat.apiResponses") : t("chat.apiChat"))}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
            <Button type="button" variant="outline" onClick={() => setEditing(blankProvider())}>
              {t("chat.add")}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}

function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: ReactNode
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-sm">{label}</span>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

function unique(names: string[]): string[] {
  const out: string[] = []
  for (const name of names) {
    const trimmed = name.trim()
    if (!trimmed || out.includes(trimmed)) continue
    out.push(trimmed)
  }
  return out
}
