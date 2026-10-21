import { describe, expect, it } from "vitest"

import { parseBuiltinTools, resolveProviderToolsDrafts } from "./provider-tools"
import type { Settings } from "./types"

describe("provider cloud tool drafts", () => {
  it("preserves provider options without enabling any tool by default", () => {
    expect(parseBuiltinTools("")).toEqual({ tools: [] })
    const tools = [{ type: "file_search", vector_store_ids: ["store-a"], filters: { type: "eq", value: 7 } }]
    expect(parseBuiltinTools(JSON.stringify(tools))).toEqual({ tools })
  })

  it.each(["{", "{}", "null", "[null]", "[[]]", '[{"type":7}]', '[{"type":" "}]'])
    ("refuses invalid JSON or tool entries: %s", (text) => {
      expect(parseBuiltinTools(text).error).toBeDefined()
    })

  it("retains drafts when the selected protocol cannot execute cloud tools", () => {
    const settings = {
      models: { default: "p", providers: [{ id: "p", api_mode: "chat_completions", builtin_tools: [] }] },
    } as unknown as Settings
    const draft = '[{"type":"code_interpreter","container":{"type":"auto"}}]'
    const result = resolveProviderToolsDrafts(settings, { p: draft })
    expect(result.errors.p).toBe("settings.models.builtinToolsNeedsResponses")
    expect(result.settings.models.providers[0].builtin_tools).toEqual(JSON.parse(draft))
    expect(settings.models.providers[0].builtin_tools).toEqual([])
  })

  it("blocks invalid JSON across providers and ignores removed providers", () => {
    const settings = {
      models: { default: "p", providers: [{ id: "p", api_mode: "responses", builtin_tools: [{ type: "file_search" }] }] },
    } as unknown as Settings
    const result = resolveProviderToolsDrafts(settings, { p: "[", removed: "[" })
    expect(Object.keys(result.errors)).toEqual(["p"])
    expect(result.settings.models.providers[0].builtin_tools).toEqual([{ type: "file_search" }])
  })

  it("allows Responses tools and a tool-free legacy provider", () => {
    const settings = {
      models: { default: "p", providers: [{ id: "p", api_mode: "responses" }, { id: "legacy" }] },
    } as unknown as Settings
    expect(resolveProviderToolsDrafts(settings, { p: '[{"type":"provider_extension"}]' }).errors).toEqual({})
    expect(resolveProviderToolsDrafts(settings, {}).settings.models.providers[1].builtin_tools).toEqual([])
  })
})
