/** Built-in composer commands. Typing `/` opens the palette at that token,
 *  the same way Cursor does — after existing text, not only at column 0.
 *  CJK punctuation IMEs emit `、` or `／` from the Slash key; those are the
 *  same prefix. Paths (`foo/bar`) and URLs (`https://`) are not a menu.
 *  The catalog is data, not a switch on ad-hoc strings in the textarea. */

import { t, type Locale } from "@/lib/i18n"

export type SlashCommandId = "goal" | "plan" | "compact"

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
    id: "plan",
    name: "plan",
    description: "Explore and write a plan before changing anything",
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

export interface SlashDraft {
  query: string
  /** Index of the slash rune in the composer text. */
  start: number
  /** Index after the command-name query (no argument yet). */
  end: number
}

/** A draft that is still naming a command: `/` plus optional letters, no space.
 *  `/goal foo` is a submit, not a menu. A CJK objective glued to the name is
 *  too: Codex cuts the name at whitespace, so `/goal持续推进` becomes an
 *  unknown name and a user task. Cursor's `/goal` skill is identifier + rest. */
export function slashDraft(text: string): SlashDraft | null {
  const start = lastTriggeredSlashIndex(text)
  if (start < 0) return null
  let end = start + 1
  while (end < text.length && isCommandNameChar(text[end] as string)) end += 1
  if (end < text.length) return null
  return { query: text.slice(start + 1, end), start, end }
}

/** CJK punctuation IMEs emit `、` (or fullwidth `／`) from the Slash key.
 *  The catalog is ASCII `/`; rewrite so the box and the filter agree. */
export function normalizeSlashPrefix(text: string): string {
  const start = lastTriggeredSlashIndex(text)
  if (start < 0 || text[start] === "/") return text
  return text.slice(0, start) + "/" + text.slice(start + 1)
}

export function stripSlashToken(
  text: string,
  draft: Pick<SlashDraft, "start" | "end">,
): string {
  return text.slice(0, draft.start) + text.slice(draft.end)
}

/** Picking `goal` / `plan` from the menu must leave `/name ` in the box.
 *  Wiping the token looks like the click missed. */
export function completeSlashCommand(
  text: string,
  draft: Pick<SlashDraft, "start" | "end">,
  name: string,
): string {
  return `${text.slice(0, draft.start)}/${name} ${text.slice(draft.end)}`
}

/** Escape on a `/goal ` prompt with no argument. */
export function clearSlashCommand(text: string): string {
  const start = lastTriggeredSlashIndex(text)
  if (start < 0) return text
  return text.slice(0, start).trimEnd()
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
  const start = lastTriggeredSlashIndex(text)
  if (start < 0) return null
  return parseSlashFrom(text.slice(start))
}

/** Codex `parse_slash_name` stops at whitespace. We stop at the first rune
 *  that is not `[A-Za-z0-9_-]`, so an IME objective glued to `/goal` is
 *  still the command. `/goals` stays a user message. */
export function splitSlash(
  text: string,
): { name: string; arg: string } | null {
  const trimmed = text.trim()
  const n = slashPrefixLength(trimmed)
  if (n <= 0) return null
  const rest = trimmed.slice(n)
  let i = 0
  while (i < rest.length && isCommandNameChar(rest[i] as string)) i += 1
  if (i === 0) return null
  return { name: rest.slice(0, i).toLowerCase(), arg: rest.slice(i).trim() }
}

function parseSlashFrom(slice: string): SlashSubmit | null {
  const parsed = splitSlash(slice)
  if (!parsed) return null
  const found = SLASH_COMMANDS.find((c) => c.name === parsed.name)
  if (!found) return null
  return { id: found.id, arg: parsed.arg }
}

function lastTriggeredSlashIndex(text: string): number {
  for (let i = text.length - 1; i >= 0; i -= 1) {
    if (!isSlashRune(text[i])) continue
    if (!isSlashTriggerBefore(i === 0 ? undefined : text[i - 1])) continue
    return i
  }
  return -1
}

function isSlashRune(ch: string | undefined): boolean {
  return ch === "/" || ch === "／" || ch === "、"
}

/** Paths and URLs keep their slashes. Whitespace and CJK before `/` open
 *  the menu, including a Chinese sentence with no ASCII space. */
function isSlashTriggerBefore(ch: string | undefined): boolean {
  if (ch == null || ch === "") return true
  if (/\s/.test(ch)) return true
  if (/[A-Za-z0-9_\-/:.]/.test(ch)) return false
  return true
}

function slashPrefixLength(text: string): number {
  if (text.startsWith("/")) return 1
  if (text.startsWith("／") || text.startsWith("、")) return 1
  return 0
}

function isCommandNameChar(ch: string): boolean {
  return /[A-Za-z0-9_-]/.test(ch)
}

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

export function commandNeedsArgument(id: SlashCommandId): boolean {
  return id === "goal" || id === "plan"
}

/** `/goal` / `/plan` with no argument yet — the box is prompting, not ready. */
export function slashAwaitingArg(text: string): SlashCommandId | null {
  const slash = parseSlashSubmit(text)
  if (!slash || !commandNeedsArgument(slash.id) || slash.arg) return null
  return slash.id
}

export function slashSubmitReady(text: string): boolean {
  const slash = parseSlashSubmit(text)
  if (!slash) return false
  return slash.id === "compact" || Boolean(slash.arg)
}

export function nextSlashIndex(
  current: number,
  count: number,
  delta: number,
): number {
  if (count <= 0) return 0
  return Math.max(0, Math.min(count - 1, current + delta))
}
