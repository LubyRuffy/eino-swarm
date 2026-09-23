import type { ModelChoice } from "./rpc"
import { httpBaseURL, normalizeApiStyle, type ApiStyle } from "./openai-wire"

const KEY = "zwai.phone.providers"

/** Idle silence, matching the desktop provider default of five minutes.
 *  Zero in a saved row is this, not "wait forever". */
export const defaultTimeoutSeconds = 300

export type DirectProvider = {
  id: string
  label: string
  baseURL: string
  apiKey: string
  api: ApiStyle
  model: string
  catalog: string[]
  timeoutSeconds: number
}

export function mintID(prefix: string): string {
  const bytes = new Uint8Array(8)
  crypto.getRandomValues(bytes)
  const hex = [...bytes].map((b) => b.toString(16).padStart(2, "0")).join("")
  return prefix + "-" + hex
}

export function blankProvider(): DirectProvider {
  return {
    id: mintID("p"),
    label: "",
    baseURL: "",
    apiKey: "",
    api: "chat",
    model: "",
    catalog: [],
    timeoutSeconds: defaultTimeoutSeconds,
  }
}

export function loadProviders(): DirectProvider[] {
  const raw = read(KEY)
  if (!raw) return []
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return []
    return parsed.map(asProvider).filter((row): row is DirectProvider => Boolean(row))
  } catch {
    return []
  }
}

export function saveProviders(rows: DirectProvider[]): DirectProvider[] {
  const clean = rows.map(normalizeProvider).filter((row) => row.baseURL)
  if (clean.length === 0) {
    remove(KEY)
    return []
  }
  write(KEY, JSON.stringify(clean))
  return clean
}

export function providerChoices(rows: DirectProvider[]): ModelChoice[] {
  const out: ModelChoice[] = []
  for (const row of rows) {
    const names = unique([row.model, ...row.catalog])
    const label = row.label.trim() || row.id
    for (const name of names) {
      out.push({
        provider_id: row.id,
        provider_label: label,
        model: name,
        default: name === row.model.trim(),
      })
    }
  }
  return out
}

export function findProvider(rows: DirectProvider[], id: string): DirectProvider | undefined {
  return rows.find((row) => row.id === id)
}

function asProvider(value: unknown): DirectProvider | null {
  if (!value || typeof value !== "object") return null
  const row = value as Partial<DirectProvider>
  const baseURL = httpBaseURL(typeof row.baseURL === "string" ? row.baseURL : "")
  if (!baseURL || typeof row.id !== "string" || !row.id.trim()) return null
  return normalizeProvider({
    id: row.id.trim(),
    label: typeof row.label === "string" ? row.label : "",
    baseURL,
    apiKey: typeof row.apiKey === "string" ? row.apiKey : "",
    api: normalizeApiStyle(typeof row.api === "string" ? row.api : ""),
    model: typeof row.model === "string" ? row.model : "",
    catalog: Array.isArray(row.catalog) ? row.catalog.filter((name) => typeof name === "string") : [],
    timeoutSeconds: typeof row.timeoutSeconds === "number" ? row.timeoutSeconds : defaultTimeoutSeconds,
  })
}

function normalizeProvider(row: DirectProvider): DirectProvider {
  const baseURL = httpBaseURL(row.baseURL) ?? ""
  const timeout = Math.floor(row.timeoutSeconds)
  return {
    ...row,
    id: row.id.trim(),
    label: row.label.trim(),
    baseURL,
    apiKey: row.apiKey,
    api: normalizeApiStyle(row.api),
    model: row.model.trim(),
    catalog: unique(row.catalog),
    timeoutSeconds: timeout >= 10 ? timeout : defaultTimeoutSeconds,
  }
}

function unique(names: string[]): string[] {
  const out: string[] = []
  const seen = new Set<string>()
  for (const name of names) {
    const trimmed = name.trim()
    if (!trimmed || seen.has(trimmed)) continue
    seen.add(trimmed)
    out.push(trimmed)
  }
  return out
}

function read(key: string): string | null {
  try {
    return localStorage.getItem(key)
  } catch {
    return null
  }
}

function write(key: string, value: string) {
  try {
    localStorage.setItem(key, value)
  } catch {
    // A full store must not crash the sheet. The in-memory row still sends.
  }
}

function remove(key: string) {
  try {
    localStorage.removeItem(key)
  } catch {
    // same as write
  }
}
