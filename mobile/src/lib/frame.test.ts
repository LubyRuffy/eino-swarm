import { describe, expect, it } from "vitest"

import { marshalFrame, unmarshalFrame, TYPE_DATA } from "./frame"

describe("frame", () => {
  it("round-trips a data frame", () => {
    const f = {
      type: TYPE_DATA,
      dst: new Uint8Array(32).fill(1),
      src: new Uint8Array(32).fill(2),
      sessionID: new Uint8Array(16).fill(3),
      payload: new Uint8Array([9, 8, 7]),
    }
    const raw = marshalFrame(f)
    const got = unmarshalFrame(raw)
    expect(got.type).toBe(TYPE_DATA)
    expect(got.dst).toEqual(f.dst)
    expect(got.src).toEqual(f.src)
    expect(got.sessionID).toEqual(f.sessionID)
    expect(got.payload).toEqual(f.payload)
  })

  it("rejects trailing junk", () => {
    const raw = marshalFrame({
      type: TYPE_DATA,
      dst: new Uint8Array(32),
      src: new Uint8Array(32),
      sessionID: new Uint8Array(16),
      payload: new Uint8Array([1]),
    })
    const padded = new Uint8Array(raw.length + 1)
    padded.set(raw)
    expect(() => unmarshalFrame(padded)).toThrow(/length/)
  })
})
