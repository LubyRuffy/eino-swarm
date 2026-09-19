import { describe, expect, it } from "vitest"

import { encodeOffer, parseOffer } from "./offer"

function pub(fill: number): Uint8Array {
  return new Uint8Array(32).fill(fill)
}

describe("offer", () => {
  it("round-trips a hub URL that contains a colon", () => {
    const uri = encodeOffer({
      hubURL: "wss://hub.example.test:2440/pairlink",
      code: "Ab3xYz9Qmn",
      hostPub: pub(0x11),
      lan: ["10.8.0.2:9100", "127.0.0.1:9100"],
    })
    const got = parseOffer(uri)
    expect(got.hubURL).toBe("wss://hub.example.test:2440/pairlink")
    expect(got.code).toBe("Ab3xYz9Qmn")
    expect(got.hostPub).toEqual(pub(0x11))
    expect(got.lan).toEqual(["10.8.0.2:9100", "127.0.0.1:9100"])
  })

  it("rejects a truncated host public key", () => {
    const uri = encodeOffer({
      hubURL: "http://127.0.0.1:7780",
      code: "ScanCode01",
      hostPub: pub(0x11),
      lan: [],
    })
    expect(() => parseOffer(uri.slice(0, -8))).toThrow(/32 bytes|host public key/)
  })

  it("rejects an unknown scheme", () => {
    expect(() => parseOffer("https://example.test/not-this")).toThrow(/not a pairlink/)
  })
})
