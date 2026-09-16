/** Built-in composer commands. Typing `/` at the start of the box lists these,
 *  the same way Cursor and Codex do. The catalog is data, not a switch on
 *  ad-hoc strings in the textarea handler. */

import { t, type Locale } from "@/lib/i18n"

export type SlashCommandId = "goal" | "compact"

export interface SlashCommand {
  id: SlashCommandId
  name: string
  description: string
  /** Extra status on the right, e.g. how full the replay looks. */
  hint?: string
}

export const SLASH_COMMANDS: readonly SlashCommand[] = [
  {
    id: "goal",
    name: "goal",
    description: "Set a standing objective to pursue until done",
  },
  {
    id: "compact",
    name: "compact",
    description: "Compact this chat's context",
  },
]

export interface SlashSubmit {
  id: SlashCommandId
  arg: string
}

/** A draft that is still naming a command: `/` plus optional letters, no space.
 *  `/goal foo` is a submit, not a menu. */
export function slashDraft(text: string): { query: string } | null {
  if (!text.startsWith("/")) return null
  if (/\s/.test(text)) return null
  return { query: text.slice(1) }
}

export function filterSlashCommands(
  query: string,
  commands: readonly SlashCommand[] = SLASH_COMMANDS,
): SlashCommand[] {
  const q = query.trim().toLowerCase()
  if (!q) return [...commands]
  return commands.filter(
    (c) =>
      c.name.startsWith(q) ||
      c.name.includes(q) ||
      c.description.toLowerCase().includes(q),
  )
}

export function parseSlashSubmit(text: string): SlashSubmit | null {
  const trimmed = text.trim()
  if (!trimmed.startsWith("/")) return null
  const rest = trimmed.slice(1)
  const space = rest.search(/\s/)
  const name = (space === -1 ? rest : rest.slice(0, space)).toLowerCase()
  const arg = space === -1 ? "" : rest.slice(space).trim()
  const cmd = SLASH_COMMANDS.find((c) => c.name === name)
  if (!cmd) return null
  return { id: cmd.id, arg }
}

export function localizedSlashCommands(locale: Locale): SlashCommand[] {
  return SLASH_COMMANDS.map((c) => ({
    ...c,
    description: t(locale, c.id === "goal" ? "slash.goal" : "slash.compact"),
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

export function commandNeedsArgument(id: SlashCommandId): boolean {
  return id === "goal"
}

export function nextSlashIndex(
  current: number,
  count: number,
  delta: number,
): number {
  if (count <= 0) return 0
  return Math.max(0, Math.min(count - 1, current + delta))
}
