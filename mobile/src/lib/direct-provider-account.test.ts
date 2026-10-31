import { beforeEach, describe, expect, it } from "vitest"
import { loadProviders, providerChoices, saveProviders, type DirectProvider } from "./direct-provider"

const privateProvider: DirectProvider = { id: "p-private", label: "Personal", baseURL: "https://private.invalid/v1", apiKey: "private-secret", api: "chat", model: "same-model", catalog: ["same-model"], timeoutSeconds: 300 }
const managed = { ...privateProvider, id: "gateway:account-one", label: "Server", baseURL: "https://gateway.invalid/v1", apiKey: "mobile-secret", sourceKind: "gateway" as const }

beforeEach(() => localStorage.clear())

describe("account model source isolation", () => {
  it("never persists a managed provider or its mobile credential in personal provider settings", () => {
    const saved = saveProviders([privateProvider, managed])
    expect(saved.map((p) => p.id)).toEqual(["p-private"])
    expect(localStorage.getItem("zwai.phone.providers")).not.toContain("mobile-secret")
    expect(loadProviders()).toEqual(saved)
  })
  it("does not rehydrate a managed source from an old personal-provider record", () => {
    localStorage.setItem("zwai.phone.providers", JSON.stringify([privateProvider, managed]))
    expect(loadProviders().map((p) => p.id)).toEqual(["p-private"])
  })
  it("drops a malformed provider ID without losing valid personal endpoints", () => {
    localStorage.setItem("zwai.phone.providers", JSON.stringify([{ ...privateProvider, id: 12 }, privateProvider]))
    expect(loadProviders().map((p) => p.id)).toEqual(["p-private"])
  })
  it("keeps identically named models in separate source identities", () => {
    const choices = providerChoices([privateProvider, managed])
    expect(choices.map((c) => [c.provider_id, c.model])).toEqual([["p-private", "same-model"], ["gateway:account-one", "same-model"]])
  })
})
