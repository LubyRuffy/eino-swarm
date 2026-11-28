import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { graphAPI } from "@/lib/graph/api"
import { publishGraphEvent } from "@/lib/graph/events"
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
  links: [
    { id: "l1", from: "s1", to: "s2", rel: "owns", found_by_todo: "t1" },
    { id: "l2", from: "s2", to: "s3", rel: "uses" },
  ],
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

  it("shows and clears in-progress rings at the same committed revision", async () => {
    const stable: GraphView = {
      ...view, todos: view.todos.map((todo) => todo.status === "doing" ? { ...todo, status: "open" } : todo),
    }
    vi.mocked(graphAPI.thread).mockResolvedValue(stable)
    render(<GraphPanel threadId="th" running />)
    await screen.findByTestId("graph-stub")
    const subjectRing = () => drawn.nodes.find((node) => node.id === "s2")?.ring
    const preview = () => publishGraphEvent("th", { kind: "preview", ownerId: "th", revision: view.revision })
    expect(subjectRing()).toBe("pending")
    vi.mocked(graphAPI.thread).mockResolvedValueOnce(view)
    await act(async () => preview())
    expect(subjectRing()).toBe("doing")
    expect(useGraph.getState().view?.revision).toBe(view.revision)
    await act(async () => preview())
    expect(subjectRing()).toBe("pending")
    expect(useGraph.getState().view?.revision).toBe(view.revision)
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

  it("starts a catch-up turn from this conversation and not while one is running", async () => {
    const onExtract = vi.fn(async () => true)
    const { rerender } = render(<GraphPanel threadId="th" onExtract={onExtract} />)
    await screen.findByTestId("graph-stub")
    fireEvent.click(screen.getByRole("button", { name: "Replay into the graph" }))
    expect(onExtract).not.toHaveBeenCalled()
    const ask = await screen.findByRole("dialog")
    expect(within(ask).queryByText(/other conversations/)).toBeNull()
    fireEvent.click(within(ask).getByRole("button", { name: "Cancel" }))
    expect(onExtract).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Replay into the graph" }))
    fireEvent.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Write on the current graph" }))
    expect(onExtract).toHaveBeenCalledWith("th", false)
    rerender(<GraphPanel threadId="th" running onExtract={onExtract} />)
    expect(screen.getByRole("button", { name: "Replay into the graph" })).toBeDisabled()
    expect(screen.getByText("Replaying this conversation round by round. The graph changes when a round is recorded.")).toBeTruthy()
    rerender(<GraphPanel projectId="pj" onExtract={onExtract} />)
    expect(screen.queryByRole("button", { name: "Replay into the graph" })).toBeNull()
  })

  it("asks before clearing a shared project graph, and skips the question when the graph is empty", async () => {
    const onExtract = vi.fn(async () => true)
    vi.mocked(graphAPI.thread).mockResolvedValue({ ...view, owner_id: "pj" })
    const { rerender } = render(<GraphPanel threadId="th" onExtract={onExtract} />)
    await screen.findByTestId("graph-stub")
    fireEvent.click(screen.getByRole("button", { name: "Replay into the graph" }))
    const ask = await screen.findByRole("dialog")
    expect(within(ask).getByText(/other conversations/)).toBeInTheDocument()
    fireEvent.click(within(ask).getByRole("button", { name: "Clear, then write" }))
    expect(onExtract).toHaveBeenCalledWith("th", true, "pj")

    vi.mocked(graphAPI.thread).mockResolvedValue({
      owner_id: "th", revision: 0, total: 0, more: false, subjects: [], links: [], todos: [],
    })
    rerender(<GraphPanel threadId="empty" onExtract={onExtract} />)
    await screen.findByText("0 subjects · 0 links")
    fireEvent.click(screen.getByRole("button", { name: "Replay into the graph" }))
    expect(onExtract).toHaveBeenLastCalledWith("empty", false)
    expect(screen.queryByRole("dialog")).toBeNull()
  })

  it("dismisses an open replay confirmation when the graph switches to a different owner", async () => {
    const onExtract = vi.fn(async () => true)
    render(<GraphPanel threadId="th" onExtract={onExtract} />)
    await screen.findByTestId("graph-stub")
    fireEvent.click(screen.getByRole("button", { name: "Replay into the graph" }))
    const previousDialog = await screen.findByRole("dialog")
    expect(within(previousDialog).queryByText(/other conversations/)).toBeNull()
    let finishRead!: (value: GraphView) => void
    vi.mocked(graphAPI.thread).mockReturnValueOnce(new Promise((resolve) => { finishRead = resolve }))
    act(() => publishGraphEvent("th", { kind: "revision", ownerId: "project", revision: view.revision }))
    expect(screen.queryByRole("dialog")).toBeNull()
    expect(screen.getByRole("button", { name: "Replay into the graph" })).toBeDisabled()
    await act(async () => finishRead({ ...view, owner_id: "project" }))
    fireEvent.click(screen.getByRole("button", { name: "Replay into the graph" }))
    expect(within(await screen.findByRole("dialog")).getByText(/other conversations/)).toBeInTheDocument()
    expect(onExtract).not.toHaveBeenCalled()
  })

  it("leaves an explanation off the graph, keeps a subject linked only to one, and opens full screen", async () => {
    vi.mocked(graphAPI.thread).mockResolvedValue({
      ...view,
      subjects: [...view.subjects, {
        id: "s4", kind: "other", name: "Option: stop the leak", attrs: { name: "Option: stop the leak" },
        status: "lead", version: 1, updated_at: "",
      }, {
        id: "s5", kind: "domain", name: "alone.example", attrs: {}, status: "lead", version: 1, updated_at: "",
      }, {
        id: "s6", kind: "other", name: "two words", attrs: { name: "two words" },
        status: "lead", version: 1, updated_at: "",
      }, {
        id: "s7", kind: "domain", name: "kept.example", attrs: {}, status: "lead", version: 1, updated_at: "",
      }],
      links: [...view.links, { id: "l3", from: "s1", to: "s6", rel: "notes" }, { id: "l4", from: "s7", to: "s4", rel: "notes" }],
    })
    render(<GraphPanel threadId="th" />)
    await screen.findByTestId("graph-stub")
    expect(drawn.nodes.map((node) => node.id)).toEqual(["s1", "s2", "s3", "s7"])
    fireEvent.click(screen.getByRole("button", { name: "Full screen" }))
    expect(await screen.findByRole("dialog")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Work graph" })).toBeInTheDocument()
  })

  it("reports a failed read and retries on request", async () => {
    vi.mocked(graphAPI.thread).mockRejectedValueOnce(new Error("offline"))
    render(<GraphPanel threadId="th" />)
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not load work records.")
    fireEvent.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByTestId("graph-stub")).toBeInTheDocument()
  })
})
