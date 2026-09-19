export function bytesToB64url(bytes: Uint8Array): string {
  let bin = ""
  for (const x of bytes) bin += String.fromCharCode(x)
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
}

export function b64urlToBytes(s: string): Uint8Array {
  const pad = s.length % 4 === 0 ? "" : "=".repeat(4 - (s.length % 4))
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/") + pad)
  const out = new Uint8Array(b.length)
  for (let i = 0; i < b.length; i++) out[i] = b.charCodeAt(i)
  return out
}

export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (x) => x.toString(16).padStart(2, "0")).join("")
}

export function hexToBytes(hex: string): Uint8Array {
  const clean = hex.trim()
  if (clean.length % 2 !== 0) throw new Error("odd hex")
  const out = new Uint8Array(clean.length / 2)
  for (let i = 0; i < out.length; i++) {
    out[i] = Number.parseInt(clean.slice(i * 2, i * 2 + 2), 16)
  }
  return out
}

export function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const n = parts.reduce((a, p) => a + p.length, 0)
  const out = new Uint8Array(n)
  let o = 0
  for (const p of parts) {
    out.set(p, o)
    o += p.length
  }
  return out
}

export function utf8(s: string): Uint8Array {
  return new TextEncoder().encode(s)
}

export function fromUtf8(b: Uint8Array): string {
  return new TextDecoder().decode(b)
}

export function randomBytes(n: number): Uint8Array {
  const out = new Uint8Array(n)
  crypto.getRandomValues(out)
  return out
}

export function putU32BE(view: DataView, offset: number, n: number) {
  view.setUint32(offset, n, false)
}

export function putU64BE(buf: Uint8Array, offset: number, n: bigint) {
  const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength)
  view.setUint32(offset, Number(n >> 32n), false)
  view.setUint32(offset + 4, Number(n & 0xffffffffn), false)
}

export function getU64BE(buf: Uint8Array, offset: number): bigint {
  const view = new DataView(buf.buffer, buf.byteOffset, buf.byteLength)
  const hi = view.getUint32(offset, false)
  const lo = view.getUint32(offset + 4, false)
  return (BigInt(hi) << 32n) | BigInt(lo)
}

export function equalBytes(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false
  let d = 0
  for (let i = 0; i < a.length; i++) d |= a[i] ^ b[i]
  return d === 0
}
