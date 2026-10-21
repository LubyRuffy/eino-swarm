import { useId } from "react"

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import type { MessageKey } from "@/lib/i18n"
import type { ProviderSettings } from "@/lib/types"
import { useT } from "@/lib/use-t"
import { cn } from "@/lib/utils"

import { Field, SettingsRow, settingsHintClass, settingsSelectTriggerClass } from "./settings-field"

export function ProviderCloudTools({ provider, query, draft, error, onUpdate, onDraftChange }: {
  provider: ProviderSettings
  query: string
  draft?: string
  error?: MessageKey
  onUpdate: (patch: Partial<ProviderSettings>) => void
  onDraftChange: (text: string) => void
}) {
  const t = useT()
  const errorId = useId()
  const toolsId = useId()
  return (
    <>
      <Field query={query} label={t("settings.models.apiMode")} hint={t("settings.models.apiModeHint")}>
        <Select value={provider.api_mode || "chat_completions"} onValueChange={(api_mode) => onUpdate({ api_mode })}>
          <SelectTrigger className={settingsSelectTriggerClass} aria-label={t("settings.models.apiMode")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="chat_completions">Chat Completions</SelectItem>
            <SelectItem value="responses">Responses</SelectItem>
          </SelectContent>
        </Select>
      </Field>
      <SettingsRow query={query} label={t("settings.models.builtinTools")} hint={t("settings.models.builtinToolsHint")} htmlFor={toolsId} controlClassName="w-1/2 min-w-64">
        <div className="w-full space-y-1.5">
          <Textarea
            id={toolsId}
            aria-label={t("settings.models.builtinTools")}
            aria-invalid={Boolean(error)}
            aria-describedby={error ? errorId : undefined}
            value={draft ?? JSON.stringify(provider.builtin_tools ?? [], null, 2)}
            rows={6}
            spellCheck={false}
            className="font-mono text-[length:var(--chrome-font-size)] shadow-none"
            onChange={(event) => onDraftChange(event.target.value)}
          />
          {error ? <p id={errorId} role="alert" className={cn(settingsHintClass, "text-destructive")}>{t(error)}</p> : null}
        </div>
      </SettingsRow>
    </>
  )
}
