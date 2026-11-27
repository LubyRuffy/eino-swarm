import { describe, expect, it, vi } from "vitest"
import {
  chooseLabels, drawLink, drawNode, drawPointerArea, ringDash, truncateLabel,
  type CanvasLink, type CanvasNode, type DrawState, type Palette,
} from "./graph-canvas"

const palette: Palette = { text: "t", muted: "m", background: "b", edge: "e", highlight: "h", slot: () => "k" }
const state = (patch: Partial<DrawState> = {}): DrawState => ({ highlighted: new Set(), selected: null, labels: new Map(), phase: 0, ...patch })
const node = (patch: Partial<CanvasNode> = {}): CanvasNode => ({ id: "a", label: "a", slot: 0, ring: "settled", radius: 9, x: 0, y: 0, ...patch })

function recorder() {
  const calls = { dash: [] as number[][], offsets: [] as number[], alpha: [] as number[] }
  let dash: number[] = []
  const ctx = {
    save: vi.fn(), restore: vi.fn(), beginPath: vi.fn(), arc: vi.fn(), fill: vi.fn(), moveTo: vi.fn(), lineTo: vi.fn(), closePath: vi.fn(),
    setLineDash(pattern: number[]) { dash = pattern },
    stroke() { calls.dash.push(dash) },
    set lineDashOffset(value: number) { calls.offsets.push(value) },
    set globalAlpha(value: number) { calls.alpha.push(value) },
  } as unknown as CanvasRenderingContext2D
  return { ctx, calls }
}

describe("the ring around a subject", () => {
  // Dashed means "not analysed yet", so only a settled ring may be solid.
  it("is solid when settled or retracted and dashed otherwise", () => {
    expect(ringDash("settled")).toEqual([])
    expect(ringDash("retracted")).toEqual([])
    for (const ring of ["unanalyzed", "pending", "doing"] as const) expect(ringDash(ring).length).toBeGreaterThan(0)
  })

  it("flows only while the subject is being analysed", () => {
    for (const ring of ["unanalyzed", "pending", "settled"] as const) {
      const { ctx, calls } = recorder()
      drawNode(node({ ring }), ctx, 1, state({ phase: 4 }), palette)
      expect(calls.offsets, ring).toEqual([0])
    }
    const { ctx, calls } = recorder()
    drawNode(node({ ring: "doing" }), ctx, 1, state({ phase: 4 }), palette)
    expect(calls.offsets).toEqual([-4])
  })

  it("greys a retracted subject and dims what a focused to-do did not touch", () => {
    const retracted = recorder()
    drawNode(node({ ring: "retracted" }), retracted.ctx, 1, state(), palette)
    expect(retracted.calls.alpha).toEqual([0.45])
    const dimmed = recorder()
    drawNode(node(), dimmed.ctx, 1, state({ highlighted: new Set(["other"]) }), palette)
    expect(dimmed.calls.alpha).toEqual([0.3])
    const marked = recorder()
    drawNode(node(), marked.ctx, 1, state({ highlighted: new Set(["a"]) }), palette)
    expect(marked.calls.alpha).toEqual([])
  })
})

describe("links", () => {
  it("are always solid, because a dashed line already means something on a node", () => {
    const { ctx, calls } = recorder()
    const link: CanvasLink = { source: node({ id: "a", x: 0 }), target: node({ id: "b", x: 100 }), retracted: false }
    drawLink(link, ctx, 1, state(), palette)
    drawLink({ ...link, retracted: true }, ctx, 1, state(), palette)
    expect(calls.dash).toEqual([[], []])
    expect(calls.alpha).toEqual([0.4])
  })
})

describe("labels", () => {
  const many = Array.from({ length: 20 }, (_, i) => node({ id: `n${i}`, x: i, y: 0 }))

  it("names everything in a small graph and only the focus in a large one", () => {
    const focus = { hovered: null, selected: null, highlighted: new Set<string>() }
    expect(chooseLabels(many.slice(0, 5), focus).size).toBe(5)
    expect(chooseLabels(many, focus).size).toBe(0)
    expect([...chooseLabels(many, { hovered: "n3", selected: "n4", highlighted: new Set(["n5"]) }).keys()].sort()).toEqual(["n3", "n4", "n5"])
  })

  it("skips nodes the layout has not placed yet", () => {
    expect(chooseLabels([node({ x: undefined })], { hovered: null, selected: null, highlighted: new Set() }).size).toBe(0)
  })

  it("shortens a long name by characters, not bytes", () => {
    expect(truncateLabel("short")).toBe("short")
    const long = "企业".repeat(20)
    expect(Array.from(truncateLabel(long)).length).toBe(22)
    expect(truncateLabel(long).endsWith("…")).toBe(true)
  })
})

describe("the pointer hit area", () => {
  // force-graph identifies the node under the cursor by colour on a hidden
  // canvas; painting the real palette there made nodes impossible to click.
  it("is one disc in exactly the colour force-graph asks for, covering the ring", () => {
    const fills: string[] = []
    const arcs: number[][] = []
    const ctx = {
      beginPath: vi.fn(), fill: vi.fn(),
      set fillStyle(value: string) { fills.push(value) },
      arc: (...args: number[]) => { arcs.push(args) },
    } as unknown as CanvasRenderingContext2D
    drawPointerArea(node({ x: 3, y: 4, radius: 9 }), "rgb(1, 2, 3)", ctx)
    expect(fills).toEqual(["rgb(1, 2, 3)"])
    expect(arcs).toHaveLength(1)
    expect(arcs[0].slice(0, 2)).toEqual([3, 4])
    expect(arcs[0][2]).toBeGreaterThan(9 * 1.35)
  })
})
