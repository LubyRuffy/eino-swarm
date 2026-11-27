import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { graphAPI } from "@/lib/graph/api"
import type { GraphView } from "@/lib/graph/types"
import { useGraph } from "@/store/graph"
import { GraphPanel } from "./graph-panel"
import type { ViewNode } from "./graph-view"

vi.mock("@/lib/graph/api", () => ({ graphAPI: { thread: vi.fn(), project: vi.fn() } }))

// force-graph needs a real canvas; the drawing is covered by graph-canvas tests
// and the browser flow by Playwright. This stand-in records what the panel asks it to draw.
const drawn = vi.hoisted(() => ({ nodes: [] as ViewNode[], highlighted: new Set<string>(), selected: null as string | null, select: (_id: string | null) => {} }))
vi.mock("./graph-view", () => ({
  GraphView: (props: { nodes: ViewNode[]; highlighted: ReadonlySet<string>; selected: string | null; onSelect: (id: string | null) => void }) => {
    drawn.nodes = props.nodes
    drawn.highlighted = new Set(props.highlighted)
    drawn.selected = props.selected
    drawn.select = props.onSelect
    return <div data-testid="graph-stub" />
  },
}))

const view: GraphView = {
  owner_id: "th", revision: 4, total: 3, more: false,
  subjects: [
    { id: "s1", kind: "company", name: "Acme", attrs: { name: "Acme" }, status: "confirmed", version: 1, updated_at: "" },
    { id: "s2", kind: "repo", name: "acme/app", attrs: { path: "acme/app", url: "https://git.example/acme/app" }, status: "lead", credentials: ["password"], found_by_todo: "t1", version: 1, updated_at: "" },
    { id: "s3", kind: "ip", name: "203.0.113.9", attrs: {}, status: "retracted", version: 1, updated_at: "" },
  ],
  links: [{ id: "l1", from: "s1", to: "s2", rel: "owns", found_by_todo: "t1" }],
  todos: [
    { id: "t2", title: "Check the repository", about: "s2", status: "doing", position: 1, version: 1 },
    { id: "t1", title: "Find the company's code", about: "s1", status: "done", position: 0, version: 1 },
    { id: "t3", title: "Verify the address", about: "s3", status: "failed", result: "host unreachable", position: 2, version: 1 },
  ],
}

beforeEach(() => {
  useGraph.setState({ owner: undefined, view: undefined, loading: false, connected: false, error: undefined })
  vi.mocked(graphAPI.thread).mockReset().mockResolvedValue(view)
})

describe("the work-record panel", () => {
  it("lists to-dos in order with done struck through and a failure's reason shown", async () => {
    render(<GraphPanel threadId="th" running />)
    const list = await screen.findByRole("list", { name: "To-dos" })
    const rows = within(list).getAllByRole("button")
    expect(rows.map((row) => row.textContent)).toEqual([
      expect.stringContaining("Find the company's code"),
      expect.stringContaining("Check the repository"),
      expect.stringContaining("Verify the address"),
    ])
    expect(within(list).getByText("Find the company's code")).toHaveClass("line-through")
    expect(within(list).getByText("Check the repository")).not.toHaveClass("line-through")
    expect(within(list).getByText("host unreachable")).toBeInTheDocument()
  })

  it("draws each subject's ring from its to-dos and the turn's running state", async () => {
    const { rerender } = render(<GraphPanel threadId="th" running />)
    await screen.findByTestId("graph-stub")
    expect(Object.fromEntries(drawn.nodes.map((node) => [node.id, node.ring]))).toEqual({ s1: "settled", s2: "doing", s3: "retracted" })
    rerender(<GraphPanel threadId="th" running={false} />)
    await waitFor(() => expect(drawn.nodes.find((node) => node.id === "s2")?.ring).toBe("pending"))
  })

  it("marks what a to-do touched, and clears it on a second click", async () => {
    render(<GraphPanel threadId="th" running />)
    const row = (await screen.findByText("Find the company's code")).closest("button")!
    fireEvent.click(row)
    expect([...drawn.highlighted].sort()).toEqual(["s1", "s2"])
    expect(row).toHaveAttribute("aria-pressed", "true")
    fireEvent.click(row)
    expect(drawn.highlighted.size).toBe(0)
  })

  it("filters the to-dos to a selected subject and shows its details", async () => {
    render(<GraphPanel threadId="th" running />)
    await screen.findByTestId("graph-stub")
    drawn.select("s2")
    const detail = await screen.findByRole("region", { name: "acme/app" })
    expect(within(detail).getByText(/Lead/)).toBeInTheDocument()
    const list = screen.getByRole("list", { name: "To-dos" })
    // Subject s2 was found by t1 and has its own to-do t2.
    expect(within(list).getAllByRole("button").map((row) => row.textContent)).toEqual([
      expect.stringContaining("Find the company's code"),
      expect.stringContaining("Check the repository"),
    ])
    fireEvent.click(screen.getByTitle("Show all to-dos"))
    expect(within(screen.getByRole("list", { name: "To-dos" })).getAllByRole("button")).toHaveLength(3)
  })

  // A credential is never sent to this reader; the panel can only say one exists.
  it("shows a stored credential as a mask and never its value", async () => {
    render(<GraphPanel threadId="th" running />)
    await screen.findByTestId("graph-stub")
    drawn.select("s2")
    const detail = await screen.findByRole("region", { name: "acme/app" })
    expect(within(detail).getByLabelText("Hidden")).toHaveTextContent("••••••")
  })

  it("reports a failed read and retries on request", async () => {
    vi.mocked(graphAPI.thread).mockRejectedValueOnce(new Error("offline"))
    render(<GraphPanel threadId="th" />)
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load work records.")
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByTestId("graph-stub")).toBeInTheDocument()
  })
})
