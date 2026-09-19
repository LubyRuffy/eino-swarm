import { describe, expect, it, vi } from "vitest"

import { redeemOffer, TICKET_PROTO } from "./client"
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
})
