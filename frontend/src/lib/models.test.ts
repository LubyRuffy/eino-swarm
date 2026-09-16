import { describe, expect, it } from "vitest"

import { groupModels, modelChoiceId, parseModelChoiceId, auxiliaryModelChoices, isTitleModelAuto, titleModelChoiceId, TITLE_MODEL_AUTO } from "./models"

describe("model choice ids", () => {
  it("round-trips a provider and a model name", () => {
    const id = modelChoiceId("default", "alpha")
    expect(parseModelChoiceId(id)).toEqual({ providerId: "default", model: "alpha" })
  })

  it("does not split on a slash inside the model name", () => {
    const id = modelChoiceId("p", "org/name")
    expect(parseModelChoiceId(id)).toEqual({ providerId: "p", model: "org/name" })
  })
})

describe("groupModels", () => {
  it("keeps server order and groups by provider", () => {
    const groups = groupModels([
      { provider_id: "a", provider_label: "One", model: "x" },
      { provider_id: "b", provider_label: "Two", model: "y" },
      { provider_id: "a", provider_label: "One", model: "z" },
    ])
    expect(groups.map((g) => g.providerId)).toEqual(["a", "b"])
    expect(groups[0].models.map((m) => m.model)).toEqual(["x", "z"])
    expect(groups[1].label).toBe("Two")
  })
})

describe("auxiliary model choices", () => {
  it("skips blank names and de-duplicates catalog plus default", () => {
    const choices = auxiliaryModelChoices([
      { id: "default", label: "Main", model: "alpha", catalog: ["alpha", "beta", ""] },
      { id: "other", label: "", model: "", catalog: ["gamma"] },
    ])
    expect(choices.map((c) => `${c.providerId}:${c.model}`)).toEqual([
      "default:alpha",
      "default:beta",
      "other:gamma",
    ])
    expect(choices[0].providerLabel).toBe("Main")
    expect(choices[2].providerLabel).toBe("other")
  })

  it("treats empty provider and model as automatic", () => {
    expect(isTitleModelAuto("", "")).toBe(true)
    expect(isTitleModelAuto("default", "alpha")).toBe(false)
    expect(titleModelChoiceId("", "", [])).toBe(TITLE_MODEL_AUTO)
    expect(
      titleModelChoiceId("default", "beta", [
        { providerId: "default", model: "beta", label: "beta", providerLabel: "Main" },
      ]),
    ).toBe(modelChoiceId("default", "beta"))
  })
})
