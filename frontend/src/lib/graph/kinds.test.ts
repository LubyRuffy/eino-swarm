import { describe, expect, it } from "vitest"
import { attrLabel, kindName, kindSlot, KINDS, OTHER_KIND } from "./kinds"
import { parseGraphFrame } from "./events"

describe("the shared kind catalog", () => {
  it("names every kind in both languages", () => {
    for (const kind of KINDS) {
      expect(kind.name.en, kind.id).toBeTruthy()
      expect(kind.name.zh, kind.id).toBeTruthy()
    }
  })

  it("falls back to the id for an unknown kind and shares one colour slot", () => {
    expect(kindName("not-a-kind", "zh")).toBe("not-a-kind")
    expect(kindSlot("not-a-kind")).toBe(KINDS.length - 1)
    expect(KINDS.at(-1)?.id).toBe(OTHER_KIND)
  })

  it("labels attributes from the catalog", () => {
    expect(attrLabel("ip", "country", "zh")).toBe("国家")
    expect(attrLabel("ip", "unknown", "en")).toBe("unknown")
  })
})

describe("graph revision frames", () => {
  it("accept only this conversation's non-negative integer revisions", () => {
    expect(parseGraphFrame('{"thread_id":"t","revision":3}', "t")).toEqual({ kind: "revision", revision: 3 })
    expect(parseGraphFrame('{"thread_id":"x","revision":3}', "t")).toBeUndefined()
    expect(parseGraphFrame('{"thread_id":"t","revision":-1}', "t")).toBeUndefined()
    expect(parseGraphFrame('{"thread_id":"t","revision":1.5}', "t")).toBeUndefined()
    expect(parseGraphFrame("nope", "t")).toBeUndefined()
  })
})
