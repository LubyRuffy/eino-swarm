import { describe, expect, it } from "vitest"

import { conversationHitValue, shouldQuerySearch } from "./thread-search"
import type { SearchHit } from "./types"

const hit = (partial: Partial<SearchHit> = {}): SearchHit => ({
  thread_id: "th_1",
  title: "gamma",
  snippet: "the vessel left",
  score: 1,
  source: "semantic",
  ...partial,
})

describe("shouldQuerySearch", () => {
  it("ignores blank input so an empty palette stays a local list", () => {
    expect(shouldQuerySearch("")).toBe(false)
    expect(shouldQuerySearch("   ")).toBe(false)
    expect(shouldQuerySearch("alpha")).toBe(true)
  })
})

describe("conversationHitValue", () => {
  it("keeps a semantic hit visible to cmdk when the query is not in the title", () => {
    const value = conversationHitValue(hit(), "the ship departed")
    expect(value).toContain("the ship departed")
    expect(value).toContain("gamma")
    expect(value).not.toMatch(/text-embedding|ada|bge/i)
  })
})
