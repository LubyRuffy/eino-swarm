import { afterEach, describe, expect, it, vi } from "vitest"

import { api } from "./api"

afterEach(() => {
  window.history.replaceState(null, "", "/")
  vi.unstubAllGlobals()
})

describe("desktop window presence", () => {
  it.each([
    ["?shell=desktop&pid=4242", "desktop", { surface: "desktop", pid: 4242 }],
    ["?shell=desktop&pid=4242", "web", { surface: "web" }],
    ["?shell=desktop", "desktop", { surface: "desktop" }],
    ["?shell=desktop&pid=0", "desktop", { surface: "desktop" }],
    ["?shell=desktop&pid=-2", "desktop", { surface: "desktop" }],
    ["?shell=desktop&pid=1.5", "desktop", { surface: "desktop" }],
    ["?shell=desktop&pid=abc", "desktop", { surface: "desktop" }],
    ["?shell=desktop&pid=4294967295", "desktop", { surface: "desktop" }],
    ["?shell=desktop&pid=9007199254740992", "desktop", { surface: "desktop" }],
  ] as const)("registers only a valid native PID (%s, %s)", async (query, surface, body) => {
    window.history.replaceState(null, "", "/" + query)
    const fetch = vi.fn(async () => ({ ok: true, json: async () => ({ id: "pc_window" }) }))
    vi.stubGlobal("fetch", fetch)
    expect(await api.presence(surface)).toEqual({ id: "pc_window" })
    const [path, options] = fetch.mock.calls[0] as unknown as [string, RequestInit]
    expect(path).toBe("/api/presence")
    expect(JSON.parse(String(options.body))).toEqual(body)
  })
})
