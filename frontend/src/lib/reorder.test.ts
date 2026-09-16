import { describe, expect, it } from "vitest"

import { applyPinnedOrder, moveItem, reorderById } from "./reorder"

describe("moveItem", () => {
  it("moves a row to a later index", () => {
    expect(moveItem(["a", "b", "c"], 0, 2)).toEqual(["b", "c", "a"])
  })

  it("moves a row to an earlier index", () => {
    expect(moveItem(["a", "b", "c"], 2, 0)).toEqual(["c", "a", "b"])
  })

  it("does not copy the list when the drop is a no-op", () => {
    const items = ["a", "b"]
    expect(moveItem(items, 1, 1)).toBe(items)
    expect(moveItem(items, -1, 0)).toBe(items)
    expect(moveItem(items, 0, 9)).toBe(items)
  })
})

describe("reorderById", () => {
  it("pins the dropped id at the target row", () => {
    expect(
      reorderById([{ id: "a" }, { id: "b" }, { id: "c" }], "c", "a"),
    ).toEqual([{ id: "c" }, { id: "a" }, { id: "b" }])
  })
})

describe("applyPinnedOrder", () => {
  it("stamps ranks and keeps unnamed rows after the drop", () => {
    expect(
      applyPinnedOrder(
        [
          { id: "a", sort_rank: 0 },
          { id: "b", sort_rank: 0 },
          { id: "c", sort_rank: 0 },
        ],
        ["b", "a"],
      ),
    ).toEqual([
      { id: "b", sort_rank: 1000 },
      { id: "a", sort_rank: 2000 },
      { id: "c", sort_rank: 0 },
    ])
  })
})
