import { describe, expect, it } from "vitest"

import { catalogToolOn, withCatalogTool } from "./tool-toggle"
import type { Settings, ToolDescriptor } from "./types"

const read: ToolDescriptor = {
  name: "read", title: "Read file", summary: "", group: "files", network: false, default_off: false,
}
const python: ToolDescriptor = {
  name: "python_runner", title: "Run Python", summary: "", group: "shell", network: false, default_off: true,
}

function tools(patch: Partial<Settings["tools"]> = {}): Settings["tools"] {
  return {
    disabled: ["exec"],
    enabled: ["screenshot"],
    proxy: { http: "http://127.0.0.1:9", https: "", no_proxy: "localhost" },
    web_search_max_results: 8,
    web_execution: "local",
    result_max_chars: 40000,
    ...patch,
  }
}

describe("catalog tool switches", () => {
  it("keeps each tool's own default and the rest of the tool settings", () => {
    const current = tools()
    expect(catalogToolOn(read, current)).toBe(true)
    expect(catalogToolOn(python, current)).toBe(false)
    expect(catalogToolOn({ ...read, name: "exec" }, current)).toBe(false)

    const off = withCatalogTool(read, false, current)
    expect(off.disabled).toContain("read")
    expect(off.disabled).toContain("exec")
    expect(off.proxy).toEqual(current.proxy)
    expect(off.web_execution).toBe("local")
    expect(catalogToolOn(read, off)).toBe(false)

    const on = withCatalogTool(python, true, off)
    expect(on.enabled).toEqual(["screenshot", "python_runner"])
    expect(catalogToolOn(python, withCatalogTool(python, false, on))).toBe(false)
    expect(catalogToolOn(read, withCatalogTool(read, true, off))).toBe(true)
  })
})
