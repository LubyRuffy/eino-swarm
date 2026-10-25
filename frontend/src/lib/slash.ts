import { t, type Locale } from "@/lib/i18n"
import { SLASH_COMMANDS, type SlashCommand, type SlashCommandId } from "./slash-core"

export * from "./slash-core"

export function localizedSlashCommands(locale: Locale): SlashCommand[] {
  return SLASH_COMMANDS.map((c) => ({
    ...c,
    description: t(
      locale,
      c.id === "goal" ? "slash.goal" : c.id === "plan" ? "slash.plan" : "slash.compact",
    ),
  }))
}

export function contextHint(chars: number, budget: number, locale: Locale = "en"): string | undefined {
  if (!(budget > 0) || !(chars >= 0)) return undefined
  const pct = Math.max(0, Math.round((100 * chars) / budget))
  return t(locale, "slash.full", { pct })
}

export function withSlashHints(
  commands: readonly SlashCommand[],
  hints: Partial<Record<SlashCommandId, string | undefined>>,
): SlashCommand[] {
  return commands.map((c) => {
    const hint = hints[c.id]
    return hint ? { ...c, hint } : { ...c }
  })
}

/** Token window first (what the meter already shows); the char budget is
 *  only a fallback when this model never reported a window. */
export function compactHint(
  tokens = 0,
  window = 0,
  chars = 0,
  budget = 0,
  locale: Locale = "en",
): string | undefined {
  if (tokens > 0 && window > 0) return contextHint(tokens, window, locale)
  if (chars > 0 && budget > 0) return contextHint(chars, budget, locale)
  return undefined
}
