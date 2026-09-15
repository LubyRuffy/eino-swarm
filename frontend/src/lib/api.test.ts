import { afterEach, describe, expect, it, vi } from "vitest"

import { api, ApiError } from "./api"

function respond(body: unknown, init?: { status?: number }) {
  const status = init?.status ?? 200
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: "",
    json: async () => body,
  } as Response
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("settings", () => {
  it("reads null tool lists as empty ones", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond({
          settings: {
            tools: { disabled: null, enabled: null, proxy: {} },
          },
        }),
      ),
    )

    const settings = await api.settings()

    expect(settings.tools.disabled).toEqual([])
    expect(settings.tools.enabled).toEqual([])
  })

  it("keeps the lists the server sent", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond({
          settings: {
            tools: { disabled: ["exec"], enabled: ["screenshot"], proxy: {} },
          },
        }),
      ),
    )

    const settings = await api.saveSettings({})

    expect(settings.tools.disabled).toEqual(["exec"])
    expect(settings.tools.enabled).toEqual(["screenshot"])
  })
})

describe("request", () => {
  it("surfaces the server's message and code", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond(
          { error: "turn already running", code: "busy" },
          { status: 409 },
        ),
      ),
    )

    await expect(api.meta()).rejects.toMatchObject({
      message: "turn already running",
      status: 409,
      code: "busy",
    })
    await expect(api.meta()).rejects.toBeInstanceOf(ApiError)
  })

  it("falls back to the status line when the body is not JSON", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 502,
        statusText: "Bad Gateway",
        json: async () => {
          throw new Error("not json")
        },
      })),
    )

    await expect(api.meta()).rejects.toMatchObject({
      message: "502 Bad Gateway",
      status: 502,
    })
  })
})
