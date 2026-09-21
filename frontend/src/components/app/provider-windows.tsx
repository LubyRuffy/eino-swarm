import { useId } from "react"

import { Input } from "@/components/ui/input"
import { parseTokenWindow, setModelWindow } from "@/lib/usage"
import type { Settings } from "@/lib/types"
import { cn } from "@/lib/utils"
import { useT } from "@/lib/use-t"

import { Field, settingsHintClass, settingsLabelClass, settingsMatch } from "./settings-field"

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
    <div className="px-4 py-3" data-settings-row="" data-testid="provider-windows">
      <p className={settingsLabelClass}>{t("settings.models.windows")}</p>
      <p className={settingsHintClass}>{t("settings.models.windowsHint")}</p>
      <ul className="mt-2.5 flex flex-col gap-1.5">
        {names.map((name, i) => {
          const id = `${uid}-${i}`
          return (
            <li key={name} className="flex items-center justify-between gap-3">
              <label htmlFor={id} className={cn(settingsLabelClass, "min-w-0 flex-1 truncate")}>
                {name}
              </label>
              <Input
                id={id}
                type="number"
                min={0}
                className="h-[32px] w-[5.75rem] px-2 text-right text-[13px] tabular-nums shadow-none"
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
        <div className="mt-3 flex items-start justify-between gap-3 border-t border-border pt-2.5">
          <div className="min-w-0 flex-1">
            <label htmlFor={`${uid}-fallback`} className={settingsLabelClass}>
              {t("settings.models.windowFallback")}
            </label>
            <p className={settingsHintClass}>
              {t("settings.models.windowFallbackHint")}
            </p>
          </div>
          <Input
            id={`${uid}-fallback`}
            type="number"
            min={0}
            className="h-[32px] w-[5.75rem] px-2 text-right text-[13px] tabular-nums shadow-none"
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
