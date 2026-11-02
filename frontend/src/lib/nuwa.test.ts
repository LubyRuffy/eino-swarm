import { describe, expect, it } from "vitest"
import { layoutNuwa, scopeNuwaNodes, type NuwaNode } from "./nuwa"

const node = (id: string, depends_on: string[] = [], thread_id = "a"): NuwaNode => ({
  id, kind: "task", title: id, summary: "", status: "pending", depends_on,
  project_id: "p", thread_id, turn_id: "t", version: 1,
  created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z",
})

describe("Shared work paths", () => {
  it("keeps dependency ordering stable when a snapshot arrives in another order", () => {
    const nodes = [node("result", ["fact"]), node("task"), node("fact", ["task"])]
    const result = layoutNuwa(nodes)
    expect(result.positions.map(({ node, column }) => [node.id, column])).toEqual([
      ["task", 0], ["fact", 1], ["result", 2],
    ])
    expect(layoutNuwa([...nodes].reverse())).toEqual(result)
    expect(nodes[0].id).toBe("result")
  })

  it("keeps every node visible when stored paths contain a cycle or missing dependency", () => {
    const result = layoutNuwa([node("a", ["b"]), node("b", ["a", "missing"]), node("c")])
    expect(result.positions.map(({ node }) => node.id).sort()).toEqual(["a", "b", "c"])
    expect(result.cyclic).toBe(true)
    expect(result.missing).toEqual(["missing"])
    expect(result.edges).toHaveLength(2)
  })

  it("retains cross-conversation prerequisites in a conversation's dependency path", () => {
    const result = scopeNuwaNodes([node("a", [], "other"), node("b", ["a"]), node("c", [], "other")], "a")
    expect(result.map((n) => n.id)).toEqual(["a", "b"])
  })
})
