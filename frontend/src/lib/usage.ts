import type { TokenTotals, UsageSnapshot } from "./types"
import { t, type Locale } from "./i18n"

const emptyTotals = {
  prompt_tokens: 0,
  completion_tokens: 0,
  cached_tokens: 0,
  reasoning_tokens: 0,
  total_tokens: 0,
  calls: 0,
}

export function emptyUsage(): UsageSnapshot {
  return {
    context_tokens: 0,
    context_window: 0,
    turn: { ...emptyTotals },
    thread: { ...emptyTotals },
  }
}

export function hasUsage(u?: UsageSnapshot | null): u is UsageSnapshot {
  if (!u) return false
  return (
    u.context_tokens > 0 ||
    u.thread.total_tokens > 0 ||
    u.turn.total_tokens > 0 ||
    u.turn.calls > 0
  )
}

export function parseUsage(text?: string): UsageSnapshot | undefined {
  if (!text) return undefined
  try {
    const raw = JSON.parse(text) as Partial<UsageSnapshot>
    if (!raw || typeof raw !== "object") return undefined
    return {
      context_tokens: num(raw.context_tokens),
      context_window: num(raw.context_window),
      turn: totals(raw.turn),
      thread: totals(raw.thread),
    }
  } catch {
    return undefined
  }
}

function totals(v: UsageSnapshot["turn"] | undefined) {
  return {
    prompt_tokens: num(v?.prompt_tokens),
    completion_tokens: num(v?.completion_tokens),
    cached_tokens: num(v?.cached_tokens),
    reasoning_tokens: num(v?.reasoning_tokens),
    total_tokens: num(v?.total_tokens),
    calls: num(v?.calls),
  }
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0
}

/** Cursor-style compact count: 850, 1.2K, 71.3K, 256K. */
export function formatTokens(n: number): string {
  const v = Math.max(0, Math.round(n))
  if (v < 1000) return String(v)
  const k = v / 1000
  if (k < 100) return trimDecimal(k.toFixed(1)) + "K"
  if (k < 1000) return `${Math.round(k)}K`
  return trimDecimal((v / 1_000_000).toFixed(1)) + "M"
}

function trimDecimal(s: string): string {
  return s.replace(/\.0$/, "")
}

export function contextPercent(used: number, window: number): number | undefined {
  if (!(window > 0) || used < 0) return undefined
  return Math.min(100, Math.round((used / window) * 100))
}

/** Arc length when the model never reported a window. `scale` is a visual
 *  half-life (the compact character budget), not a token limit — an empty
 *  ring looks frozen, but claiming 54.1K/80K tokens would be a unit lie. */
export function unknownWindowFill(used: number, scale: number): number {
  if (!(used > 0) || !(scale > 0)) return 0
  return 1 - 1 / (1 + used / scale)
}

export function meterFill(
  used: number,
  window: number,
  scale = 0,
): number {
  const pct = contextPercent(used, window)
  if (pct !== undefined) return pct / 100
  return unknownWindowFill(used, scale)
}

type ModelWindow = {
  provider_id: string
  model: string
  default?: boolean
  context_window?: number
}

/** Token limit for the composer's current selection. A name that is not in
 *  the catalog still uses that provider's default row so a typed-in model
 *  is not a dead ring. A listed name with window 0 stays 0 — stealing a
 *  sibling's discovered limit would invent from the wrong name. */
export function windowForSelection(
  models: readonly ModelWindow[],
  provider?: string,
  model?: string,
): number {
  const group = models.filter((m) => !provider || m.provider_id === provider)
  const pool = group.length > 0 ? group : models
  if (model) {
    const named = pool.find((m) => m.model === model)
    if (named) return named.context_window ?? 0
  }
  const fallback = pool.find((m) => m.default) ?? pool[0]
  return fallback?.context_window ?? 0
}

/** Codex-style turn line: `12K in · 3.1K out · 4K cached`. */
export function formatTurnBits(totals: TokenTotals, locale: Locale = "en"): string | undefined {
  if (totals.calls <= 0 && totals.total_tokens <= 0) return undefined
  const bits = [
    t(locale, "usage.in", { n: formatTokens(totals.prompt_tokens) }),
    t(locale, "usage.out", { n: formatTokens(totals.completion_tokens) }),
  ]
  if (totals.cached_tokens > 0) {
    bits.push(t(locale, "usage.cached", { n: formatTokens(totals.cached_tokens) }))
  }
  if (totals.reasoning_tokens > 0) {
    bits.push(t(locale, "usage.thinking", { n: formatTokens(totals.reasoning_tokens) }))
  }
  return bits.join(" · ")
}

export function mergeModelContext(
  current?: Record<string, number>,
  incoming?: Record<string, number>,
): Record<string, number> {
  const out = { ...(current ?? {}) }
  for (const [name, n] of Object.entries(incoming ?? {})) {
    const key = name.trim()
    if (!key || !(n > 0)) continue
    out[key] = n
  }
  return out
}

/** Write one name's window. Zero clears that name so the provider fallback
 *  can apply, instead of pinning a fake 0 over a sibling's limit. */
export function setModelWindow(
  current: Record<string, number> | undefined,
  name: string,
  n: number,
): Record<string, number> {
  const key = name.trim()
  const out = { ...(current ?? {}) }
  if (!key) return out
  if (n > 0) out[key] = Math.round(n)
  else delete out[key]
  return out
}

export function parseTokenWindow(raw: string): number {
  const n = Number(raw)
  return Number.isFinite(n) && n > 0 ? Math.round(n) : 0
}
