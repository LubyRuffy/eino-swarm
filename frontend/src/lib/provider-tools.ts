import type { MessageKey } from "./i18n"
import type { Settings } from "./types"

export type ProviderToolsDrafts = Record<string, string>

export function parseBuiltinTools(text: string): {
  tools?: Array<Record<string, unknown>>
  error?: MessageKey
} {
  let parsed: unknown
  try {
    parsed = JSON.parse(text.trim() || "[]")
  } catch {
    return { error: "settings.models.builtinToolsInvalid" }
  }
  if (!Array.isArray(parsed) || parsed.some((tool) =>
    typeof tool !== "object" || tool === null || Array.isArray(tool) ||
    typeof tool.type !== "string" || !tool.type.trim(),
  )) return { error: "settings.models.builtinToolsInvalid" }
  return { tools: parsed }
}

/** Raw JSON stays outside Settings so an incomplete edit never becomes an
 *  API payload. The parent sheet retains it when its Models page unmounts. */
export function resolveProviderToolsDrafts(settings: Settings, drafts: ProviderToolsDrafts) {
  const errors: Record<string, MessageKey> = {}
  const providers = settings.models.providers.map((provider) => {
    const parsed = drafts[provider.id] === undefined
      ? { tools: provider.builtin_tools ?? [] }
      : parseBuiltinTools(drafts[provider.id])
    if (parsed.error) {
      errors[provider.id] = parsed.error
      return provider
    }
    if (parsed.tools?.length && provider.api_mode !== "responses") {
      errors[provider.id] = "settings.models.builtinToolsNeedsResponses"
    }
    return { ...provider, builtin_tools: parsed.tools }
  })
  return { settings: { ...settings, models: { ...settings.models, providers } }, errors }
}
