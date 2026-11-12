import { describe, expect, it } from "vitest"
import { assignMCPServerID, createMCPDraft, mcpDraftInput, mcpTransportKey } from "./mcp-settings"

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

  it("assigns an internal id without using the display name", () => {
    expect(assignMCPServerID("kept-id", ["other"], () => { throw new Error("unused") })).toBe("kept-id")
    expect(assignMCPServerID("remote.example", [], () => Uint8Array.of(0xbb))).toBe("bb")
    expect(assignMCPServerID("aa", ["aa"], () => Uint8Array.of(0xbb))).toBe("bb")
    let attempt = 0
    expect(assignMCPServerID("", ["aa"], () => Uint8Array.of(attempt++ === 0 ? 0xaa : 0xbb))).toBe("bb")
    expect(assignMCPServerID("", [], () => new Uint8Array(16).fill(0x0f))).toBe("0f".repeat(16))
    expect(assignMCPServerID("", [])).toMatch(/^[a-f0-9]{32}$/)
    expect(() => assignMCPServerID("", [], () => new Uint8Array())).toThrow(/allocation failed/)
    expect(mcpTransportKey("stdio")).toBe("mcp.stdio")
    expect(mcpTransportKey("streamable_http")).toBe("mcp.http")
    expect(mcpTransportKey("sse")).toBe("mcp.sse")
  })
})
