import { useId } from "react"

import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { parseTokenWindow, setModelWindow } from "@/lib/usage"
import type { Settings } from "@/lib/types"
import { useT } from "@/lib/use-t"

import { Field, settingsMatch } from "./settings-field"

type ProviderConfig = Settings["models"]["providers"][number]

/** Per-name token limits for one endpoint. A single box for the whole
 *  provider would pin every model to the same window, which is a lie the
 *  moment the catalog has more than one name. */
export function ProviderWindows({
  query,
  names,
  provider,
  onUpdate,
}: {
  query: string
  names: string[]
  provider: ProviderConfig
  onUpdate: (patch: Partial<ProviderConfig>) => void
}) {
  const t = useT()
  const uid = useId()
  const haystack = [
    t("settings.models.window"),
    t("settings.models.windowHint"),
    t("settings.models.windows"),
    t("settings.models.windowsHint"),
    t("settings.models.windowFallback"),
    t("settings.models.windowFallbackHint"),
    ...names,
  ]
  if (!settingsMatch(query, ...haystack)) return null

  const writeName = (name: string, raw: string) => {
    onUpdate({
      model_context: setModelWindow(
        provider.model_context,
        name,
        parseTokenWindow(raw),
      ),
    })
  }

  const writeFallback = (raw: string) => {
    onUpdate({ context_window: parseTokenWindow(raw) })
  }

  if (names.length === 0) {
    return (
      <Field
        query={query}
        label={t("settings.models.window")}
        hint={t("settings.models.windowHint")}
      >
        <Input
          type="number"
          min={0}
          value={provider.context_window || ""}
          placeholder={t("settings.models.windowPlaceholder")}
          onChange={(e) => writeFallback(e.target.value)}
        />
      </Field>
    )
  }

  return (
    <div className="px-4 py-3.5" data-settings-row="" data-testid="provider-windows">
      <p className="text-sm font-medium">{t("settings.models.windows")}</p>
      <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
        {t("settings.models.windowsHint")}
      </p>
      <ul className="mt-3 flex flex-col gap-2">
        {names.map((name, i) => {
          const id = `${uid}-${i}`
          return (
            <li key={name} className="flex items-center justify-between gap-3">
              <Label htmlFor={id} className="min-w-0 flex-1 truncate font-normal">
                {name}
              </Label>
              <Input
                id={id}
                type="number"
                min={0}
                className="h-9 w-28"
                aria-label={t("settings.models.windowFor", { name })}
                value={provider.model_context?.[name] || ""}
                placeholder={t("settings.models.windowPlaceholder")}
                onChange={(e) => writeName(name, e.target.value)}
              />
            </li>
          )
        })}
      </ul>
      {names.length > 1 ? (
        <div className="mt-4 flex items-start justify-between gap-3 border-t border-border pt-3">
          <div className="min-w-0 flex-1">
            <Label htmlFor={`${uid}-fallback`}>
              {t("settings.models.windowFallback")}
            </Label>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              {t("settings.models.windowFallbackHint")}
            </p>
          </div>
          <Input
            id={`${uid}-fallback`}
            type="number"
            min={0}
            className="h-9 w-28"
            aria-label={t("settings.models.windowFallback")}
            value={provider.context_window || ""}
            placeholder={t("settings.models.windowPlaceholder")}
            onChange={(e) => writeFallback(e.target.value)}
          />
        </div>
      ) : null}
    </div>
  )
}
