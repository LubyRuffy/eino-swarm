import { b64urlToBytes, bytesToB64url } from "./bytes"

export const SCHEME = "pairlink:v1:"

export type Offer = {
  hubURL: string
  code: string
  hostPub: Uint8Array
  lan: string[]
}

export function encodeOffer(o: Offer): string {
  if (!o.hubURL.trim() || !o.code.trim()) {
    throw new Error("incomplete offer")
  }
  if (o.hostPub.length !== 32) {
    throw new Error("host public key must be 32 bytes")
  }
  if (/[:?&]/.test(o.code)) {
    throw new Error("pairing code must not contain : ? &")
  }
  let s = SCHEME + o.hubURL + ":" + o.code + ":" + bytesToB64url(o.hostPub)
  if (o.lan.length > 0) {
    const q = new URLSearchParams()
    q.set("lan", o.lan.join(","))
    s += "?" + q.toString()
  }
  return s
}

export function parseOffer(raw: string): Offer {
  const text = raw.trim()
  if (!text.startsWith(SCHEME)) {
    throw new Error("not a pairlink v1 offer")
  }
  let rest = text.slice(SCHEME.length)
  let lanRaw = ""
  const qAt = rest.indexOf("?")
  if (qAt >= 0) {
    const q = new URLSearchParams(rest.slice(qAt + 1))
    lanRaw = q.get("lan") ?? ""
    rest = rest.slice(0, qAt)
  }
  const spkAt = rest.lastIndexOf(":")
  if (spkAt <= 0) throw new Error("missing host public key")
  const spk = rest.slice(spkAt + 1)
  rest = rest.slice(0, spkAt)
  const codeAt = rest.lastIndexOf(":")
  if (codeAt <= 0) throw new Error("missing pairing code")
  const code = rest.slice(codeAt + 1)
  const hubURL = rest.slice(0, codeAt)
  if (!hubURL || !code || !spk) throw new Error("incomplete offer")
  const hostPub = b64urlToBytes(spk)
  if (hostPub.length !== 32) {
    throw new Error("host public key must be 32 bytes")
  }
  const lan = lanRaw
    .split(",")
    .map((p) => p.trim())
    .filter(Boolean)
  return { hubURL, code, hostPub, lan }
}
