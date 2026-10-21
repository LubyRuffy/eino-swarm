import { describe, expect, it } from "vitest"

import { cloudToolSummary, parseCloudTool } from "./cloud-tool"

describe("cloud tool metadata", () => {
  it("preserves complete cloud diagnostics and localizes the source label", () => {
    const metadata = { type: "file_search_call", status: "completed", action: { query: "topic" }, provider_id: "endpoint-a" }
    expect(parseCloudTool(JSON.stringify(metadata))).toEqual(metadata)
    expect(cloudToolSummary(metadata)).toBe("Cloud tool: file_search_call · completed")
    expect(cloudToolSummary(metadata, "zh")).toBe("云端工具：file_search_call · completed")
  })

  it.each([undefined, "invalid", "null", "[]", "1", '"text"'])("handles unusable metadata: %s", (text) => {
    expect(parseCloudTool(text)).toEqual({})
  })

  it("does not render non-string metadata as UI markup or object text", () => {
    expect(cloudToolSummary({ type: {}, status: [] })).toBe("Cloud tool: ? · ?")
  })
})
