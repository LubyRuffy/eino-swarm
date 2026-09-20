import { describe, expect, it, vi } from "vitest"

import { bindError, deliverResponse, hubError, redeemOffer, TICKET_PROTO } from "./client"
import type { RemoteResponse } from "./rpc"
import { generateIdentity } from "./crypto"
import { bytesToB64url, bytesToHex } from "./bytes"

describe("redeemOffer", () => {
  it("posts the device public key and reads ticket fields", async () => {
    const device = generateIdentity()
    const hostPub = generateIdentity().pub
    const sid = new Uint8Array(16).fill(7)
    const fetcher = vi.fn(async () => {
      return new Response(
        JSON.stringify({
          ticket: "abc123",
          host_pub: bytesToB64url(hostPub),
          session_id: bytesToHex(sid),
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      )
    })
    const got = await redeemOffer(
      {
        hubURL: "http://127.0.0.1:7780",
        code: "ScanCode01",
        hostPub,
        lan: [],
      },
      device,
      fetcher as unknown as typeof fetch,
    )
    expect(got.ticket).toBe("abc123")
    expect(got.hostPub).toEqual(hostPub)
    expect(fetcher).toHaveBeenCalledOnce()
    expect(JSON.stringify(fetcher.mock.calls)).toContain("pairings/redeem")
    expect(TICKET_PROTO).toBe("pairlink.ticket.")
  })

  it("unwraps a hub JSON error so the scan screen is not a raw blob", async () => {
    const device = generateIdentity()
    const fetcher = vi.fn(async () => {
      return new Response(JSON.stringify({ error: "host offline" }), { status: 409 })
    })
    await expect(
      redeemOffer(
        {
          hubURL: "http://127.0.0.1:7780",
          code: "ScanCode01",
          hostPub: generateIdentity().pub,
          lan: [],
        },
        device,
        fetcher as unknown as typeof fetch,
      ),
    ).rejects.toThrow("host offline")
  })
})

describe("hubError", () => {
  it("prefers JSON error, then raw text, then the status", () => {
    expect(hubError(409, '{"error":"host offline"}').message).toBe("host offline")
    expect(hubError(500, "not-json").message).toBe("not-json")
    expect(hubError(502, "{").message).toBe("{")
    expect(hubError(503, "   ").message).toBe("503")
  })
})

describe("bindError", () => {
  it("maps a host-offline redeem to the scan copy", async () => {
    const { setLocale, t } = await import("./i18n")
    setLocale("en")
    expect(bindError(new Error("host offline"))).toBe(t("scan.hostOffline"))
    expect(bindError("ws timeout")).toBe("ws timeout")
  })
})

describe("deliverResponse", () => {
  it("routes a matching id to the waiter and unmatched frames to onPush", () => {
    const pending = new Map<string, (r: RemoteResponse) => void>()
    const waiter = vi.fn()
    const push = vi.fn()
    pending.set("m1", waiter)
    const rpc: RemoteResponse = { v: 1, id: "m1", ok: true }
    deliverResponse(pending, rpc, push)
    expect(waiter).toHaveBeenCalledWith(rpc)
    expect(push).not.toHaveBeenCalled()
    const event: RemoteResponse = {
      v: 1,
      id: "",
      ok: true,
      op: "event",
      thread_id: "t",
    }
    deliverResponse(pending, event, push)
    expect(push).toHaveBeenCalledWith(event)
  })
})
