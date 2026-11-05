import { describe, expect, it } from "vitest"
import { createMCPDraft, mcpDraftInput } from "./mcp-settings"

describe("MCP configuration drafts", () => {
  it("keeps saved credentials write-only and preserves them when unchanged", () => {
    const draft = createMCPDraft({
      id: "local", name: "Local", enabled: true, transport: "stdio", command: "runner",
      args: ["--serve", "a b"], url: "", timeout_seconds: 30,
      env_keys: ["TOKEN"], header_keys: [],
    })
    expect(draft.env).toBe("")
    expect(mcpDraftInput(draft)).toEqual({
      id: "local", name: "Local", enabled: true, transport: "stdio", command: "runner",
      args: ["--serve", "a b"], url: "", timeout_seconds: 30,
    })
  })

  it("distinguishes clearing credentials from retaining them and refuses invalid JSON", () => {
    const draft = createMCPDraft()
    draft.id = "remote"
    draft.name = "Remote"
    draft.transport = "streamable_http"
    draft.url = "https://service.invalid/mcp"
    draft.env = "{}"
    draft.headers = '{"Authorization":"private"}'
    expect(mcpDraftInput(draft)).toMatchObject({ env: {}, headers: { Authorization: "private" } })
    for (const value of ["[]", "null", '{"TOKEN":4}', "bad"]) {
      expect(() => mcpDraftInput({ ...draft, env: value })).toThrow()
    }
    expect(() => mcpDraftInput({ ...draft, args: "not json" })).toThrow()
    expect(() => mcpDraftInput({ ...draft, args: "[1]" })).toThrow()
  })
})
