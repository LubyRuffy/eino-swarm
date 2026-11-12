import { describe, expect, it } from "vitest"

import {
  accountChoiceLabel, accountKindTally, accountModelRows, accountModelsProblem, accountModelsStale,
  effectiveAccountModel, embeddingModelChoices, officialModelLock, serverStartsConversations, showWelcome,
} from "./account-models"
import type { AccountModels, Meta, ModelInfo } from "./types"

const meta = (patch: Partial<Meta>): Meta => ({
  version: "test", mode: "desktop", mock: false, configured: false, default_provider: "",
  reasoning_levels: [], data_dir: "", capabilities: {}, swarm: {} as Meta["swarm"], ...patch,
})
const account = (state: AccountModels["state"], reason?: AccountModels["reason"]): AccountModels =>
  ({ state, reason, models: 0 })

describe("first-run welcome", () => {
  it("shows only when nothing can run and the person has not answered", () => {
    expect(showWelcome(undefined)).toBe(false)
    expect(showWelcome(meta({}))).toBe(true)
    // Models from a signed-in account make the install usable, so no welcome.
    expect(showWelcome(meta({ configured: true }))).toBe(false)
    // Putting it off sticks: the next launch must not nag again.
    expect(showWelcome(meta({ ui: { welcome_dismissed: true } as Meta["ui"] }))).toBe(false)
    // A restoring engine has not told us whether models exist yet.
    expect(showWelcome(meta({ startup: "restoring" }))).toBe(false)
  })
})

describe("which source a new conversation starts on", () => {
  it("uses the server when its models are ready and the person has not chosen a provider", () => {
    expect(serverStartsConversations("account", true)).toBe(true)
    expect(serverStartsConversations(undefined, true)).toBe(true)
    // A chosen provider stays the start even while the server is ready.
    expect(serverStartsConversations("provider", true)).toBe(false)
    // Until the server can answer, the configured provider is the start.
    expect(serverStartsConversations("account", false)).toBe(false)
    expect(serverStartsConversations(undefined, false)).toBe(false)
  })
})

describe("account model problems", () => {
  it("explains a missing catalog by its stable code, never by server text", () => {
    expect(accountModelsProblem(account("ready"))).toBeUndefined()
    expect(accountModelsProblem(account("signed_out"))).toBeUndefined()
    expect(accountModelsProblem(account("pending"))).toBeUndefined()
    expect(accountModelsProblem(account("no_access"))).toBe("accountModels.noAccess")
    expect(accountModelsProblem(account("unavailable", "unsupported"))).toBe("accountModels.reason.unsupported")
    expect(accountModelsProblem(account("unavailable", "rejected"))).toBe("accountModels.reason.rejected")
    // A reason this build does not know still gets a sentence.
    expect(accountModelsProblem({ ...account("unavailable"), reason: "from-the-future" as never }))
      .toBe("accountModels.reason.unknown")
  })
})

describe("account model rows", () => {
  it("keeps only the server's ready models, in the server's order", () => {
    const row = (provider_id: string, model: string, patch: Partial<ModelInfo> = {}): ModelInfo =>
      ({ id: provider_id + model, provider_id, provider_label: provider_id, label: model, model, ready: true, ...patch })
    const rows = accountModelRows([
      row("gateway:a", "m1", { source: "account" }),
      row("own", "x", { source: "provider" }),
      row("gateway:a", "m2", { source: "account" }),
      row("gateway:a", "", { source: "account" }),
      row("gateway:a", "m3", { source: "account", ready: false }),
    ])
    expect(rows.map((r) => r.model)).toEqual(["m1", "m2"])
  })

  it("counts only the types the server reported and keeps a saved conversation default", () => {
    expect(accountKindTally(undefined)).toEqual([])
    expect(accountKindTally({
      chat: 1, unclassified: 2, embedding: 1, ocr: 0, decisions: 0, other: 0,
    }).map((part) => part.key)).toEqual(["chat", "unclassified", "embedding"])
    const row = (model: string, selected = false): ModelInfo =>
      ({ id: model, provider_id: "gateway:a", provider_label: "Server", label: model, model, ready: true, source: "account", default: selected })
    const models = [row("first", true), row("second")]
    expect(effectiveAccountModel(models, "")).toBe("first")
    expect(effectiveAccountModel(models, "second")).toBe("second")
    expect(effectiveAccountModel(models, "missing")).toBe("missing")
    const branded = [{ ...row("account"), label: "Partner model" }]
    expect(accountChoiceLabel(branded, "vendor-secret-model")).toBe("Partner model")
    expect(accountChoiceLabel(branded, "")).toBe("Partner model")
    expect(accountChoiceLabel(models, "missing")).toBe("missing")
    expect(officialModelLock({ state: "ready", models: 1, profile: { presentation: "official", instruction: false } })).toBe(true)
    expect(officialModelLock({ state: "ready", models: 1, profile: { presentation: "full", instruction: false } })).toBe(false)
    expect(officialModelLock(account("ready"))).toBe(false)
    expect(embeddingModelChoices({
      state: "ready", models: 1, source_id: "gateway:a", label: "Server",
      kinds: { chat: 0, embedding: 1, ocr: 0, decisions: 0, other: 0, unclassified: 0, embedding_models: ["embed-a", "embed-a"] },
    })).toEqual([{ providerId: "gateway:a", model: "embed-a", label: "embed-a", providerLabel: "Server" }])
    expect(embeddingModelChoices({ state: "ready", models: 1 })).toEqual([])
  })
})

describe("stale account models", () => {
  it("looks again only while sign-in state and the model list disagree", () => {
    expect(accountModelsStale("authorized", account("signed_out"))).toBe(true)
    expect(accountModelsStale("authorized", account("pending"))).toBe(true)
    expect(accountModelsStale("authorized", account("ready"))).toBe(false)
    expect(accountModelsStale("authorized", account("no_access"))).toBe(false)
    expect(accountModelsStale("authorized", account("unavailable", "unsupported"))).toBe(false)
    expect(accountModelsStale("signed_out", account("ready"))).toBe(true)
    expect(accountModelsStale("signed_out", account("signed_out"))).toBe(false)
    // Mid-login the list is whatever it was; nothing to wait for.
    expect(accountModelsStale("authorization_pending", account("signed_out"))).toBe(false)
    expect(accountModelsStale(undefined, account("ready"))).toBe(false)
    expect(accountModelsStale("authorized", undefined)).toBe(false)
  })
})
