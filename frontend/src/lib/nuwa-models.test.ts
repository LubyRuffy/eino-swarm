import { describe, expect, it } from "vitest"

import { defaultNuwaModelPair, nuwaPlannerPatch, savedNuwaPlanner } from "./nuwa-models"
import type { ModelInfo, Project } from "./types"

const ready = (providerId: string, model: string, isDefault = false): ModelInfo => ({
  id: `${providerId}\t${model}`,
  provider_id: providerId,
  provider_label: providerId,
  label: model,
  model,
  ready: true,
  default: isDefault,
})

const project = {
  id: "pj",
  name: "Shared",
  system_prompt: "",
  workdir: "",
  resolved_workdir: "",
  memory_enabled: false,
  memory_dir: "",
  nuwa_enabled: true,
  nuwa_planner_provider_id: "planner-endpoint",
  nuwa_planner_model: "planner-model",
  nuwa_executor_provider_id: "executor-endpoint",
  nuwa_executor_model: "executor-model",
  created_at: "",
  updated_at: "",
} satisfies Project

describe("default Nuwa model pair", () => {
  it("uses the conversation model and the next ready model", () => {
    const pair = defaultNuwaModelPair(
      [ready("a", "alpha", true), ready("a", "beta"), ready("b", "gamma")],
      { providerId: "b", model: "gamma" },
    )
    expect(pair.planner).toEqual({ providerId: "b", model: "gamma" })
    expect(pair.executor).toEqual({ providerId: "a", model: "alpha" })
  })

  it("uses the installed default and then the same model when it is the only one", () => {
    const pair = defaultNuwaModelPair([ready("a", "alpha", true), { ...ready("a", "hidden"), ready: false }])
    expect(pair.planner).toEqual({ providerId: "a", model: "alpha" })
    expect(pair.executor).toEqual({ providerId: "a", model: "alpha" })
  })

  it("treats an empty conversation model as that provider's default", () => {
    const pair = defaultNuwaModelPair(
      [ready("a", "alpha", true), ready("b", "beta"), ready("b", "gamma", true)],
      { providerId: "b", model: "" },
    )
    expect(pair.planner).toEqual({ providerId: "b", model: "gamma" })
    expect(pair.executor).toEqual({ providerId: "a", model: "alpha" })
  })
})

describe("saved Nuwa planner", () => {
  it("is the project planner only while Nuwa is on and both models are saved", () => {
    expect(savedNuwaPlanner(project)).toEqual({ providerId: "planner-endpoint", model: "planner-model" })
    expect(savedNuwaPlanner({ ...project, nuwa_enabled: false })).toBeUndefined()
    expect(savedNuwaPlanner({ ...project, nuwa_executor_model: "" })).toBeUndefined()
  })

  it("changes the planner without dropping the executor", () => {
    expect(nuwaPlannerPatch(project, "planner-endpoint", "other-planner")).toEqual({
      nuwa_planner_provider_id: "planner-endpoint",
      nuwa_planner_model: "other-planner",
      nuwa_executor_provider_id: "executor-endpoint",
      nuwa_executor_model: "executor-model",
    })
  })
})
