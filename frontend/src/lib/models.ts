import type { ModelInfo } from "./types"

/** Selection key for a provider+model pair. Opaque to the UI. */
export function modelChoiceId(providerId: string, model: string): string {
  return `${providerId}\t${model}`
}

export function parseModelChoiceId(id: string): { providerId: string; model: string } {
  const tab = id.indexOf("\t")
  if (tab < 0) return { providerId: id, model: "" }
  return { providerId: id.slice(0, tab), model: id.slice(tab + 1) }
}

export interface GroupedModels<T extends { provider_id: string; provider_label?: string }> {
  providerId: string
  label: string
  models: T[]
}

/** Keep provider order as the server sent it; the composer groups by that. */
export function groupModels<T extends { provider_id: string; provider_label?: string }>(
  models: T[],
): GroupedModels<T>[] {
  const groups: GroupedModels<T>[] = []
  const index = new Map<string, number>()
  for (const m of models) {
    let i = index.get(m.provider_id)
    if (i === undefined) {
      i = groups.length
      index.set(m.provider_id, i)
      groups.push({
        providerId: m.provider_id,
        label: m.provider_label || m.provider_id,
        models: [],
      })
    }
    groups[i].models.push(m)
  }
  return groups
}

/** Sentinel for "follow this conversation's model". Radix Select rejects "". */
export const TITLE_MODEL_AUTO = "auto"

export interface AuxiliaryChoice {
  providerId: string
  model: string
  label: string
  providerLabel: string
}

/** Flatten every named model on every endpoint. Empty names are skipped. */
export function auxiliaryModelChoices(
  providers: Array<{
    id: string
    label?: string
    model?: string
    catalog?: string[]
  }>,
): AuxiliaryChoice[] {
  const out: AuxiliaryChoice[] = []
  const seen = new Set<string>()
  for (const p of providers) {
    const providerLabel = p.label?.trim() || p.id
    const names = [p.model ?? "", ...(p.catalog ?? [])]
    for (const raw of names) {
      const model = raw.trim()
      if (!model) continue
      const id = modelChoiceId(p.id, model)
      if (seen.has(id)) continue
      seen.add(id)
      out.push({ providerId: p.id, model, label: model, providerLabel })
    }
  }
  return out
}

export function isTitleModelAuto(provider?: string, model?: string): boolean {
  return !provider?.trim() && !model?.trim()
}

export function titleModelChoiceId(
  provider?: string,
  model?: string,
  choices: AuxiliaryChoice[] = [],
): string {
  if (isTitleModelAuto(provider, model)) return TITLE_MODEL_AUTO
  const hit = choices.find(
    (c) =>
      (!provider?.trim() || c.providerId === provider) &&
      c.model === model,
  )
  if (hit) return modelChoiceId(hit.providerId, hit.model)
  if (provider && model) return modelChoiceId(provider, model)
  return TITLE_MODEL_AUTO
}

/** The row the composer button shows. An empty conversation model follows
 *  the default row, including one that is not ready. A stored name that left
 *  the list is not replaced. */
export function displayedModel(
  models: ModelInfo[],
  providerId?: string,
  model?: string,
): ModelInfo | undefined {
  const named = model?.trim() ?? ""
  const rows = models.filter((row) => row.model)
  if (providerId && named) {
    return rows.find((row) => row.provider_id === providerId && row.model === named)
  }
  const providerListed = !providerId || rows.some((row) => row.provider_id === providerId)
  if (!providerListed || named) return undefined
  const same = providerId ? rows.filter((row) => row.provider_id === providerId) : rows
  return (
    same.find((row) => row.default) ??
    same.find((row) => row.ready) ??
    rows.find((row) => row.default) ??
    rows.find((row) => row.ready)
  )
}

/** A catalog-order default is remembered on the conversation. A user setting
 *  is not: later changes to that setting must still apply. */
export function rememberedCatalogModel(
  row: ModelInfo | undefined,
  storedModel?: string,
): { providerId: string; model: string } | undefined {
  if (storedModel?.trim()) return undefined
  if (!row?.default || row.default_follows_setting || !row.ready || !row.model || !row.provider_id) return undefined
  return { providerId: row.provider_id, model: row.model }
}

/** Name to send with a turn when the conversation has not stored one. The
 *  server remembers it only when no user-configured default applies. */
export function composerRequestModel(
  models: ModelInfo[],
  providerId?: string,
  storedModel?: string,
): string | undefined {
  if (storedModel?.trim()) return undefined
  const name = displayedModel(models, providerId, "")?.model.trim()
  return name || undefined
}
