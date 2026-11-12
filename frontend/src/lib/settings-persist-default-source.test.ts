import { describe, expect, it } from "vitest"

import { persistPayload } from "./settings-persist"
import type { Settings } from "./types"

const provider = {
  id: "default", label: "Endpoint", base_url: "http://endpoint.invalid/v1", model: "m", catalog: [],
  timeout_seconds: 300, has_api_key: true, ready: true,
}
const base = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: { default: "default", default_source: "account", providers: [provider] },
  swarm: {
    max_concurrent: 1, agent_timeout_seconds: 1, max_turns: 1, manager_max_iterations: 1,
    progress_interval_seconds: 1, delta_coalesce_ms: 1, auto_title: true,
  },
  tools: { disabled: [], enabled: [], proxy: { http: "", https: "", no_proxy: "" }, web_search_max_results: 8 },
  memory: {
    enabled: true, auto_review: true, char_limit: 1, entry_max: 1, review_max_iterations: 1,
    skills_index_max: 1, notifications: "on",
  },
  log: { level: "info" },
} as Settings

describe("default model source in a settings save", () => {
  it("is sent only when this draft changed it", () => {
    const edited = persistPayload(
      { ...base, models: { ...base.models, providers: [{ ...provider, model: "other" }] } }, undefined, base,
    )
    // Editing a provider elsewhere must not carry a stale source along: the
    // server keeps what it has when the field is absent.
    expect(edited.models && "default_source" in edited.models).toBe(false)

    const chosen = persistPayload(
      { ...base, models: { ...base.models, default_source: "provider" } }, undefined, base,
    )
    expect(chosen.models?.default_source).toBe("provider")
    const presented = persistPayload(
      { ...base, models: { ...base.models, account_presentation: "full" } }, undefined, base,
    )
    expect(presented.models?.account_presentation).toBe("full")
    const untouched = persistPayload(
      { ...base, models: { ...base.models, account_presentation: "official", providers: [{ ...provider, model: "other" }] } },
      undefined,
      { ...base, models: { ...base.models, account_presentation: "official" } },
    )
    expect(untouched.models && "account_presentation" in untouched.models).toBe(false)

    const named = persistPayload(
      { ...base, models: { ...base.models, account_model: "second" } }, undefined, base,
    )
    expect(named.models?.account_model).toBe("second")
    const editedAgain = persistPayload(
      { ...base, models: { ...base.models, account_model: "second", providers: [{ ...provider, model: "other" }] } },
      undefined,
      { ...base, models: { ...base.models, account_model: "second" } },
    )
    expect(editedAgain.models && "account_model" in editedAgain.models).toBe(false)
  })

  it("is never invented for an older server that does not report it", () => {
    const legacy = { ...base, models: { ...base.models, default_source: undefined } } as Settings
    const patch = persistPayload(
      { ...legacy, models: { ...legacy.models, providers: [{ ...provider, model: "other" }] } }, undefined, legacy,
    )
    expect(patch.models && "default_source" in patch.models).toBe(false)
  })
})
