import type { ProjectPatch } from "@/lib/api"
import type { ModelInfo, Project } from "@/lib/types"

export interface NuwaModelChoice {
  providerId: string
  model: string
}

export interface NuwaModelPairChoice {
  planner?: NuwaModelChoice
  executor?: NuwaModelChoice
}

function readyModels(models: ModelInfo[]): ModelInfo[] {
  return models.filter((row) => row.ready && row.provider_id.trim() !== "" && row.model.trim() !== "")
}

function sameChoice(row: ModelInfo, choice: NuwaModelChoice): boolean {
  return row.provider_id === choice.providerId && row.model === choice.model
}

// Catalog order, with no ranking. A conversation hint wins; otherwise the
// first ready default, then the first ready row. The executor is the next
// different ready row, or the planner when the catalog has only one.
export function defaultNuwaModelPair(
  models: ModelInfo[],
  hint?: { providerId?: string; model?: string },
): NuwaModelPairChoice {
  const ready = readyModels(models)
  if (ready.length === 0) return {}
  const providerId = hint?.providerId?.trim() ?? ""
  const model = hint?.model?.trim() ?? ""
  const hinted = providerId
    ? ready.find((row) => row.provider_id === providerId && (model ? row.model === model : row.default)) ??
      ready.find((row) => row.provider_id === providerId && row.default) ??
      ready.find((row) => row.provider_id === providerId)
    : undefined
  const planner = hinted ?? ready.find((row) => row.default) ?? ready[0]
  const executor = ready.find((row) => !sameChoice(row, { providerId: planner.provider_id, model: planner.model })) ?? planner
  return {
    planner: { providerId: planner.provider_id, model: planner.model },
    executor: { providerId: executor.provider_id, model: executor.model },
  }
}

export function savedNuwaPlanner(project?: Project | null): NuwaModelChoice | undefined {
  if (!project?.nuwa_enabled) return undefined
  const providerId = project.nuwa_planner_provider_id?.trim() ?? ""
  const model = project.nuwa_planner_model?.trim() ?? ""
  const executorProviderId = project.nuwa_executor_provider_id?.trim() ?? ""
  const executorModel = project.nuwa_executor_model?.trim() ?? ""
  if (!providerId || !model || !executorProviderId || !executorModel) return undefined
  return { providerId, model }
}

export function nuwaPlannerPatch(project: Project, providerId: string, model: string): ProjectPatch {
  return {
    nuwa_planner_provider_id: providerId,
    nuwa_planner_model: model,
    nuwa_executor_provider_id: project.nuwa_executor_provider_id,
    nuwa_executor_model: project.nuwa_executor_model,
  }
}
