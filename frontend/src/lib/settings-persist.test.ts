import { afterEach, describe, expect, it, vi } from "vitest"

import {
  SETTINGS_SAVE_DEBOUNCE_MS,
  SettingsPersist,
  persistPayload,
} from "./settings-persist"
import type { Settings } from "./types"

const base: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: {
    default: "default",
    providers: [
      {
        id: "default",
        label: "Endpoint",
        base_url: "http://endpoint.invalid/v1",
        model: "",
        catalog: [],
        timeout_seconds: 300,
        has_api_key: false,
        ready: false,
      },
    ],
  },
  swarm: {
    max_concurrent: 1,
    agent_timeout_seconds: 1,
    max_turns: 1,
    manager_max_iterations: 1,
    progress_interval_seconds: 1,
    delta_coalesce_ms: 1,
    auto_title: true,
  },
  tools: {
    disabled: [],
    enabled: [],
    proxy: { http: "", https: "", no_proxy: "" },
    web_search_max_results: 8,
  },
  memory: {
    enabled: true,
    auto_review: true,
    char_limit: 1,
    entry_max: 1,
    review_max_iterations: 1,
    skills_index_max: 1,
    notifications: "on",
  },
  log: { level: "info" },
}

describe("persistPayload", () => {
  it("keeps the locale with the rest of the document", () => {
    const patch = persistPayload(base, "zh")
    expect(patch.memory).toEqual(base.memory)
    expect(patch.ui).toEqual({
      locale: "zh",
      font: "system",
      font_size: "medium",
      content_width: "comfortable",
    })
  })

  it("keeps a typeface already on the document", () => {
    const patch = persistPayload(
      {
        ...base,
        ui: {
          locale: "en",
          font: "serif",
          font_size: "large",
          content_width: "full",
        },
      },
      "zh",
    )
    expect(patch.ui).toEqual({
      locale: "zh",
      font: "serif",
      font_size: "large",
      content_width: "full",
    })
  })
})

describe("SettingsPersist", () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it("does not write until the debounce has elapsed", async () => {
    vi.useFakeTimers()
    const write = vi.fn().mockResolvedValue(undefined)
    const persist = new SettingsPersist(write)
    persist.schedule({ ...base, log: { level: "debug" } })
    expect(write).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(SETTINGS_SAVE_DEBOUNCE_MS - 1)
    expect(write).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(write).toHaveBeenCalledTimes(1)
    expect(write.mock.calls[0][0].log).toEqual({ level: "debug" })
  })

  // Typing a URL or spinning a number must not rewrite config.yaml per key.
  it("coalesces edits into one write", async () => {
    vi.useFakeTimers()
    const write = vi.fn().mockResolvedValue(undefined)
    const persist = new SettingsPersist(write)
    persist.schedule({ ...base, swarm: { ...base.swarm, max_concurrent: 2 } })
    persist.schedule({ ...base, swarm: { ...base.swarm, max_concurrent: 3 } })
    await vi.advanceTimersByTimeAsync(SETTINGS_SAVE_DEBOUNCE_MS)
    expect(write).toHaveBeenCalledTimes(1)
    expect(write.mock.calls[0][0].swarm.max_concurrent).toBe(3)
  })

  it("flush writes the pending document immediately", async () => {
    vi.useFakeTimers()
    const write = vi.fn().mockResolvedValue(undefined)
    const persist = new SettingsPersist(write)
    persist.schedule({ ...base, log: { level: "warn" } })
    await persist.flush()
    expect(write).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(SETTINGS_SAVE_DEBOUNCE_MS)
    expect(write).toHaveBeenCalledTimes(1)
  })

  it("flush is a no-op when nothing is pending", async () => {
    const write = vi.fn().mockResolvedValue(undefined)
    const persist = new SettingsPersist(write)
    await persist.flush()
    expect(write).not.toHaveBeenCalled()
  })

  // Back to app must not drop the last keystrokes because the timer was killed.
  it("dispose without flush drops an unwritten edit", async () => {
    vi.useFakeTimers()
    const write = vi.fn().mockResolvedValue(undefined)
    const persist = new SettingsPersist(write)
    persist.schedule({ ...base, log: { level: "error" } })
    persist.dispose()
    await vi.advanceTimersByTimeAsync(SETTINGS_SAVE_DEBOUNCE_MS)
    expect(write).not.toHaveBeenCalled()
  })

  // A language click PUTs ui immediately; a still-pending swarm edit must
  // not flush the previous pin a beat later and undo it.
  it("uses the language at write time, not at the first keystroke", async () => {
    vi.useFakeTimers()
    const write = vi.fn().mockResolvedValue(undefined)
    const persist = new SettingsPersist(write)
    persist.schedule({ ...base, log: { level: "debug" } }, "en")
    persist.setLocale("zh")
    await persist.flush()
    expect(write).toHaveBeenCalledTimes(1)
    expect(write.mock.calls[0][0].ui).toEqual({
      locale: "zh",
      font: "system",
      font_size: "medium",
      content_width: "comfortable",
    })
    expect(write.mock.calls[0][0].log).toEqual({ level: "debug" })
  })

  it("a failed write does not block the next one", async () => {
    vi.useFakeTimers()
    const write = vi
      .fn()
      .mockRejectedValueOnce(new Error("disk full"))
      .mockResolvedValueOnce(undefined)
    const persist = new SettingsPersist(write)
    persist.schedule({ ...base, log: { level: "debug" } })
    await vi.advanceTimersByTimeAsync(SETTINGS_SAVE_DEBOUNCE_MS)
    await expect(persist.flush()).rejects.toThrow("disk full")
    persist.schedule({ ...base, log: { level: "warn" } })
    await vi.advanceTimersByTimeAsync(SETTINGS_SAVE_DEBOUNCE_MS)
    expect(write).toHaveBeenCalledTimes(2)
  })
})
