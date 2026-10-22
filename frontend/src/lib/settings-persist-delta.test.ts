import { describe, expect, it, vi } from "vitest"
import { SettingsPersist, persistPayload } from "./settings-persist"
import { normalizeUISettings } from "./appearance"
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

const copy = () => structuredClone(base)
function pending() { let resolve!: () => void; const promise = new Promise<void>((done) => { resolve = done }); return { promise, resolve } }

describe("desktop saves preserve separately changed PC timeouts", () => {
  it("sends only the changed font section", () => {
    const draft = copy(); draft.ui = normalizeUISettings({ font: "serif" })
    expect(Object.keys(persistPayload(draft, undefined, base))).toEqual(["ui"])
  })
  it("omits untouched worker duration when another swarm field changes", () => {
    const draft = copy(); draft.swarm.max_concurrent = 2
    const patch = persistPayload(draft, undefined, base)
    expect(Object.keys(patch)).toEqual(["swarm"])
    expect(patch.swarm?.max_concurrent).toBe(2)
    expect(patch.swarm).not.toHaveProperty("agent_timeout_seconds")
  })
  it("compares provider duration by ID while labels and order change", () => {
    const before = copy(); before.models.providers[0].first_byte_timeout_seconds = 30
    before.models.providers.push({ ...before.models.providers[0], id: "second", timeout_seconds: 90, first_byte_timeout_seconds: 12 })
    const draft = structuredClone(before); draft.models.providers.reverse(); draft.models.providers[1].label = "Renamed"
    const patch = persistPayload(draft, undefined, before)
    expect(patch.models?.providers.map((p) => p.id)).toEqual(["second", "default"])
    for (const p of patch.models!.providers) {
      expect(p).not.toHaveProperty("timeout_seconds"); expect(p).not.toHaveProperty("first_byte_timeout_seconds")
    }
  })
  it("retains explicit duration edits and a new provider's own duration", () => {
    const draft = copy(); draft.swarm.agent_timeout_seconds = 72
    draft.models.providers[0].timeout_seconds = 91
    draft.models.providers[0].first_byte_timeout_seconds = 19
    draft.models.providers.push({ ...draft.models.providers[0], id: "new" })
    const patch = persistPayload(draft, undefined, base)
    expect(patch.swarm?.agent_timeout_seconds).toBe(72)
    expect(patch.models?.providers[0]).toMatchObject({ timeout_seconds: 91, first_byte_timeout_seconds: 19 })
    expect(patch.models?.providers[1]).toMatchObject({ id: "new", timeout_seconds: 91, first_byte_timeout_seconds: 19 })
  })
  it("computes queued B-to-A reversion after the successful B write", async () => {
    const hold = pending(); const write = vi.fn().mockReturnValueOnce(hold.promise).mockResolvedValue(undefined)
    const persist = new SettingsPersist(write, 400, base)
    const b = copy(); b.log.level = "debug"
    persist.schedule(b); const first = persist.flush(); await Promise.resolve()
    persist.schedule(copy()); const second = persist.flush()
    hold.resolve(); await first; await second
    expect(write).toHaveBeenCalledTimes(2)
    expect(write.mock.calls[1][0]).toEqual({ log: { level: "info" } })
  })
  it("a failed write does not advance the dirty baseline", async () => {
    const write = vi.fn().mockRejectedValueOnce(new Error("disk full")).mockResolvedValue(undefined)
    const persist = new SettingsPersist(write, 400, base)
    const b = copy(); b.swarm.agent_timeout_seconds = 72
    persist.schedule(b); await expect(persist.flush()).rejects.toThrow("disk full")
    persist.schedule(b); await persist.flush()
    expect(write).toHaveBeenCalledTimes(2)
    expect(write.mock.calls[1][0]).toEqual(write.mock.calls[0][0])
    expect(write.mock.calls[1][0].swarm.agent_timeout_seconds).toBe(72)
  })
  it("uses the submitted draft, not concurrent values from a server response", async () => {
    const response = copy(); response.swarm.agent_timeout_seconds = 73
    const write = vi.fn().mockResolvedValue(response); const persist = new SettingsPersist(write, 400, base)
    const b = copy(); b.log.level = "debug"; persist.schedule(b); await persist.flush()
    const c = structuredClone(b); c.personality = { instructions: "brief" }; persist.schedule(c); await persist.flush()
    expect(write.mock.calls[1][0]).toEqual({ personality: { instructions: "brief" } })
  })
  it("never writes before an initial settings baseline was loaded", async () => {
    const write = vi.fn().mockResolvedValue(undefined); const persist = new SettingsPersist(write)
    const draft = copy(); draft.log.level = "debug"; persist.schedule(draft); await persist.flush()
    expect(write).not.toHaveBeenCalled()
  })
  it("keeps the submitted locale baseline when a later locale is queued mid-write", async () => {
    const hold = pending(); const write = vi.fn().mockReturnValueOnce(hold.promise).mockResolvedValue(undefined)
    const persist = new SettingsPersist(write, 400, base)
    const draft = copy(); draft.log.level = "debug"
    persist.schedule(draft, "en"); const first = persist.flush(); await Promise.resolve()
    persist.setLocale("zh"); persist.schedule(draft); const second = persist.flush()
    hold.resolve(); await first; await second
    expect(write.mock.calls[0][0].ui.locale).toBe("en")
    expect(write.mock.calls[1][0]).toEqual({ ui: expect.objectContaining({ locale: "zh" }) })
  })

})
