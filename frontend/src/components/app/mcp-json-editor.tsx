import { useId } from "react"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { useT } from "@/lib/use-t"
import { SettingsSection, settingsHintClass, settingsLabelClass } from "./settings-field"

export function MCPJSONEditor({ text, saving, error, onChange, onApply, onCancel }: {
  text: string
  saving: boolean
  error: string
  onChange: (text: string) => void
  onApply: () => void
  onCancel: () => void
}) {
  const t = useT()
  const id = useId()
  const hintId = useId()
  const asTyped = { autoComplete: "off", autoCapitalize: "off", autoCorrect: "off", spellCheck: false } as const
  return (
    <SettingsSection title={t("mcp.jsonTitle")}>
      <fieldset disabled={saving} className="min-w-0" data-settings-row="">
        <div className="flex flex-col gap-3 px-4 py-4">
          <div>
            <label htmlFor={id} className={settingsLabelClass}>{t("mcp.jsonLabel")}</label>
            <p id={hintId} className={settingsHintClass}>{t("mcp.jsonHint")}</p>
          </div>
          <Textarea id={id} value={text} rows={12} aria-describedby={hintId} {...asTyped}
            className="w-full font-mono text-xs" onChange={(event) => onChange(event.target.value)} />
          {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="ghost" onClick={onCancel}>{t("mcp.cancel")}</Button>
            <Button type="button" onClick={onApply}>{t(saving ? "mcp.jsonApplying" : "mcp.jsonApply")}</Button>
          </div>
        </div>
      </fieldset>
    </SettingsSection>
  )
}
