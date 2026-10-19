import { cleanup, render, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { openSaved, type DeviceLink } from "@/lib/client"
import type { RemoteRequest } from "@/lib/rpc"
import { saveLink } from "@/lib/store"

vi.mock("@/lib/client", async (original) => ({
  ...await original<typeof import("@/lib/client")>(), openSaved: vi.fn(),
}))
import { App } from "./app"

afterEach(() => { cleanup(); vi.mocked(openSaved).mockReset() })

describe("attention-only activity refresh", () => {
  it.each(["running", "waiting"] as const)("hides and restores an unread result through the real inbox poll when %s changes", async (activity) => {
    saveLink({ hubURL: "http://127.0.0.1:9", ticket: "t", hostPub: "p", sessionID: "s", fingerprint: "fp" })
    let active = false
    const calls: Partial<RemoteRequest>[] = []
    const link = {
      path: "relay", alive: () => true, close: vi.fn(), announceDevice: async () => undefined,
      rpc: async (request: Partial<RemoteRequest>) => {
        calls.push(request)
        return { v: 1, id: "r", ok: true, threads: [], running: [], attention: [{
          id: "attention-only", title: "Retained result", running: false, waiting: false,
          [activity]: active, last_active_at: "2026-01-01T00:00:00Z", unread: true, completed_seq: 17,
        }] }
      },
    } as unknown as DeviceLink
    vi.mocked(openSaved).mockResolvedValue(link)
    render(<App />)
    const result = { name: "Open Retained result" }
    expect(await screen.findByRole("button", result)).toBeInTheDocument()

    // No component rerender: the periodic list response passes through App.applyList's fingerprint guard.
    active = true
    await waitFor(() => expect(screen.queryByRole("button", result)).not.toBeInTheDocument(), { timeout: 3500 })
    expect(calls.some((request) => request.op === "open" || request.op === "read")).toBe(false)
    active = false
    expect(await screen.findByRole("button", result, { timeout: 3500 })).toBeInTheDocument()
    expect(calls.filter((request) => request.op === "list").length).toBeGreaterThanOrEqual(3)
    expect(calls.some((request) => request.op === "open" || request.op === "read")).toBe(false)
  }, 10000)
})
