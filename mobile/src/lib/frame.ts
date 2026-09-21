import { putU32BE } from "./bytes"

export const MAGIC = "PLK1"
export const TYPE_HANDSHAKE = 0x01
export const TYPE_DATA = 0x02
export const TYPE_DISCO = 0x03
export const TYPE_OBSERVED = 0x04
export const TYPE_PUNCH_PING = 0x10
export const KEY_SIZE = 32
export const SESSION_ID_SIZE = 16
export const MAX_PAYLOAD = 64 << 10
export const HEADER_SIZE = 89

export type Frame = {
  type: number
  dst: Uint8Array
  src: Uint8Array
  sessionID: Uint8Array
  payload: Uint8Array
}

export function marshalFrame(f: Frame): Uint8Array {
  if (f.payload.length > MAX_PAYLOAD) throw new Error("payload too large")
  if (f.dst.length !== KEY_SIZE || f.src.length !== KEY_SIZE) {
    throw new Error("bad key")
  }
  if (f.sessionID.length !== SESSION_ID_SIZE) throw new Error("bad session id")
  const out = new Uint8Array(HEADER_SIZE + f.payload.length)
  out[0] = 80
  out[1] = 76
  out[2] = 75
  out[3] = 49
  out[4] = f.type
  out.set(f.dst, 5)
  out.set(f.src, 37)
  out.set(f.sessionID, 69)
  putU32BE(new DataView(out.buffer), 85, f.payload.length)
  out.set(f.payload, 89)
  return out
}

export function unmarshalFrame(b: Uint8Array): Frame {
  if (b.length < HEADER_SIZE) throw new Error("short frame")
  if (
    b[0] !== 80 ||
    b[1] !== 76 ||
    b[2] !== 75 ||
    b[3] !== 49
  ) {
    throw new Error("bad magic")
  }
  const n = new DataView(b.buffer, b.byteOffset, b.byteLength).getUint32(85, false)
  if (n > MAX_PAYLOAD) throw new Error("payload too large")
  if (b.length !== HEADER_SIZE + n) throw new Error("length mismatch")
  return {
    type: b[4],
    dst: b.slice(5, 37),
    src: b.slice(37, 69),
    sessionID: b.slice(69, 85),
    payload: b.slice(89),
  }
}

export function httpToWS(hub: string): string {
  const u = new URL(hub)
  if (u.protocol === "https:") u.protocol = "wss:"
  else if (u.protocol === "http:") u.protocol = "ws:"
  const path = u.pathname.replace(/\/$/, "")
  u.pathname = path + "/pairlink/v1/ws"
  u.search = ""
  u.hash = ""
  return u.toString()
}

export function originURL(hub: string): string {
  return hub.replace(/\/+$/, "")
}
