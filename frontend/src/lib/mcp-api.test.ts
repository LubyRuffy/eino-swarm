import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "./api"
import type { MCPServerInput } from "./mcp-settings"

const input: MCPServerInput = {
  id: "local", name: "Local", transport: "stdio", command: "runner", args: [], url: "",
  enabled: true, timeout_seconds: 0,
}

afterEach(() => vi.unstubAllGlobals())

describe("MCP configuration API writes", () => {
  it("makes new connections conditional so another client's same ID cannot be overwritten", async () => {
    const fetcher = vi.fn().mockImplementation(async () => new Response(JSON.stringify({ server: input })))
    vi.stubGlobal("fetch", fetcher)
    await api.saveMCPServer(input, true)
    expect(fetcher).toHaveBeenLastCalledWith("/api/mcp/local", expect.objectContaining({
      method: "PUT", headers: { "Content-Type": "application/json", "If-None-Match": "*" },
    }))
    await api.saveMCPServer(input)
    expect(fetcher).toHaveBeenLastCalledWith("/api/mcp/local", expect.objectContaining({
      method: "PUT", headers: { "Content-Type": "application/json" },
    }))
  })
})
