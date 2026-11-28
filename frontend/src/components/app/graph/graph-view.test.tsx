import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { GraphView } from "./graph-view"

const captured = vi.hoisted(() => ({ nodeLabel: (_node: { label: string }) => "unset" }))

vi.mock("force-graph", () => ({
  default: function ForceGraph() {
    let data = { nodes: [] as { id: string }[], links: [] as { retracted?: boolean }[] }
    const proxy: object = new Proxy({}, {
      get(_target, prop) {
        if (prop === "graphData") {
          return (next?: typeof data) => {
            if (next) data = next
            return next ? proxy : data
          }
        }
        if (prop === "nodeLabel") return (fn: typeof captured.nodeLabel) => { captured.nodeLabel = fn; return proxy }
        return () => proxy
      },
    })
    return proxy
  },
}))

const text = { graph: "Graph", empty: "Empty", fit: "Fit", zoomIn: "In", zoomOut: "Out" }

describe("the side-panel graph", () => {
  it("keeps its height as a minimum so a long list cannot squeeze it", () => {
    render(<GraphView nodes={[]} links={[]} highlighted={new Set()} selected={null} text={text} onSelect={() => {}} />)
    const stage = screen.getByRole("group", { name: "Graph" }).parentElement!
    expect(stage).toHaveClass("shrink-0")
    expect(stage).toHaveStyle({ height: "280px", minHeight: "280px" })
  })

  it("names a node on the canvas only, with no second label under the pointer", () => {
    render(<GraphView nodes={[{ id: "n", label: "one subject", slot: 0, ring: "settled" }]} links={[]} highlighted={new Set()} selected={null} text={text} onSelect={() => {}} />)
    expect(captured.nodeLabel({ label: "one subject" })).toBe("")
  })

  it("fills the enlarged dialog instead of locking that minimum", () => {
    render(<GraphView nodes={[]} links={[]} highlighted={new Set()} selected={null} fill text={text} onSelect={() => {}} />)
    const stage = screen.getByRole("group", { name: "Graph" }).parentElement!
    expect(stage).not.toHaveClass("shrink-0")
    expect(stage).not.toHaveStyle({ minHeight: "280px" })
  })
})
