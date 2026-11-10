import { describe, expect, it } from "vitest"
import { compareNuwaRecent, layoutNuwa, layoutNuwaCollaboration, nuwaLooseGroupID, nuwaPathFocus, nuwaStatusTally, NUWA_GOAL_ID, scopeNuwaNodes, type NuwaNode } from "./nuwa"

const node = (id: string, depends_on: string[] = [], thread_id = "a", kind = "task"): NuwaNode => ({
  id, kind, title: id, summary: "", status: "pending", depends_on,
  project_id: "p", thread_id, turn_id: "t", version: 1,
  created_at: `2026-01-01T00:00:${id.length.toString().padStart(2, "0")}Z`, updated_at: "2026-01-01T00:00:00Z",
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

  it("collapses records with no dependency into one goal arrow per kind", () => {
    const result = layoutNuwaCollaboration([
      node("task"), node("fact", ["task"], "a", "fact"), node("aside"), node("note"),
    ])
    const goal = result.positions.find(({ node }) => node.type === "goal")
    expect(goal).toBeTruthy()
    const toGoal = result.edges.filter(({ to }) => to === NUWA_GOAL_ID).map(({ from }) => from).sort()
    expect(toGoal).toEqual([nuwaLooseGroupID("task"), "fact"].sort())
    expect(result.edges).toContainEqual({ from: "task", to: "fact" })
    const loose = result.positions.find(({ node }) => node.type === "loose")
    expect(loose?.node.type === "loose" && loose.node.records.map((record) => record.id).sort()).toEqual(["aside", "note"])
  })

  it("keeps a dense report readable by drawing the dependency path instead of every result", () => {
    const looseArtifacts = Array.from({ length: 16 }, (_, index) => node(`artifact-${index}`, [], "a", "artifact"))
    const looseFacts = Array.from({ length: 8 }, (_, index) => node(`fact-${index}`, [], "a", "fact"))
    const chain = [
      node("audit", [], "a", "task"),
      node("cve", ["audit"], "a", "fact"),
      node("verify", ["cve"], "a", "artifact"),
      node("matrix", [], "a", "fact"),
      node("poc", ["matrix"], "a", "artifact"),
    ]
    const result = layoutNuwaCollaboration([...looseArtifacts, ...looseFacts, node("loose-task"), ...chain])
    const cards = result.positions.flatMap(({ node }) => node.type === "work" ? [node.id] : [])
    const groups = result.positions.flatMap(({ node }) => node.type === "loose" ? [[node.kind, node.records.length]] : [])
    expect(cards.sort()).toEqual(["audit", "cve", "loose-task", "matrix", "poc", "verify"])
    expect(groups).toEqual([["fact", 8], ["artifact", 16]])
    const toGoal = result.edges.filter(({ to }) => to === NUWA_GOAL_ID).map(({ from }) => from).sort()
    expect(toGoal).toEqual([
      nuwaLooseGroupID("artifact"), nuwaLooseGroupID("fact"), "loose-task", "poc", "verify",
    ].sort())
    expect(result.edges.filter(({ to }) => to !== NUWA_GOAL_ID).sort((a, b) => a.from.localeCompare(b.from))).toEqual([
      { from: "audit", to: "cve" },
      { from: "cve", to: "verify" },
      { from: "matrix", to: "poc" },
    ])
    expect(nuwaPathFocus(chain, "poc")).toEqual(new Set(["matrix", "poc"]))
    expect(nuwaPathFocus([...looseArtifacts, ...chain], looseArtifacts[3].id)).toEqual(new Set([
      nuwaLooseGroupID("artifact"), looseArtifacts[3].id,
    ]))
    expect(nuwaPathFocus(chain, "cve")).toEqual(new Set(["audit", "cve", "verify"]))
    expect(nuwaPathFocus([...looseArtifacts, ...looseFacts, node("loose-task"), ...chain], "loose-task")).toEqual(new Set(["loose-task"]))
    expect(nuwaStatusTally([
      node("a", [], "a", "artifact"),
      { ...node("b", [], "a", "artifact"), status: "reported" },
      { ...node("c", [], "a", "artifact"), status: "reported" },
    ])).toEqual([{ status: "reported", count: 2 }, { status: "pending", count: 1 }])
    expect(compareNuwaRecent(
      { ...node("older"), updated_at: "2026-01-01T00:00:00Z" },
      { ...node("newer"), updated_at: "2026-02-01T00:00:00Z" },
    )).toBeGreaterThan(0)
  })

  it("adds no goal node for an empty collaboration graph", () => {
    const result = layoutNuwaCollaboration([])
    expect(result.positions).toHaveLength(0)
    expect(result.edges).toHaveLength(0)
  })
})
