import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { NuwaPanel } from "./nuwa-panel"
import { NuwaGraph } from "./nuwa-graph"
import { useApp } from "@/store/app"
import { useTraceFocus } from "@/store/trace-focus"
import { nuwaAPI, subscribeNuwa } from "@/lib/nuwa-api"
import type { NuwaNode, NuwaSnapshot } from "@/lib/nuwa"
import type { Project } from "@/lib/types"

vi.mock("@/lib/nuwa-api", () => ({ nuwaAPI: { snapshot: vi.fn() }, subscribeNuwa: vi.fn() }))
const project: Project = {
  id: "p", name: "Shared project", system_prompt: "", workdir: "", resolved_workdir: "",
  memory_enabled: false, memory_dir: "", nuwa_enabled: true, created_at: "", updated_at: "",
}
const node = (id: string, patch: Partial<NuwaNode> = {}): NuwaNode => ({
  id, title: `Node ${id}`, kind: "task", summary: `Summary ${id}`, status: "pending", version: 1,
  depends_on: [], thread_id: "a", turn_id: "turn1", created_at: "", updated_at: "", ...patch,
})
const snapshot: NuwaSnapshot = {
  project_id: "p", revision: 1,
  nodes: [
    node("plan", { status: "done" }),
    node("finding", { kind: "fact", status: "reported", depends_on: ["plan"], detail: "Full source evidence", evidence: [{ thread_id: "a", turn_id: "turn1", seq: 17, label: "Measured result" }] }),
    node("output", { kind: "artifact", status: "reported", depends_on: ["finding"], thread_id: "b", artifact_path: "results/report.md" }),
  ],
  workers: [{ agent_id: "researcher-1", role: "researcher", status: "running", thread_id: "a", turn_id: "turn1", parent_id: "coordinator-1", model_binding: { ref: "analysis", provider_id: "endpoint", model: "chosen-model", reasoning_effort: "high" } }],
}
let handlers: Parameters<typeof subscribeNuwa>[1]
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(nuwaAPI.snapshot).mockResolvedValue(snapshot)
  vi.mocked(subscribeNuwa).mockImplementation((_id, next) => { handlers = next; return vi.fn() })
  useApp.setState({ activeId: "a", loadUntilTurn: vi.fn().mockResolvedValue(true) })
  useTraceFocus.getState().clear()
})

describe("Nuwa shared work", () => {
  it("keeps a direct turn visible when the collaboration graph is empty", async () => {
    vi.mocked(nuwaAPI.snapshot).mockResolvedValue({ ...snapshot, nodes: [], workers: [] })
    const onOpenFiles = vi.fn()
    const props = { project, threadId: "a", onSelectAgent: vi.fn() }
    const activity = { id: "direct-turn", status: "running", onOpenFiles }
    const { rerender } = render(<NuwaPanel {...props} activity={activity} />)
    await screen.findByText("Shared work will appear here as agents record tasks, facts, and results.")
    const section = screen.getByRole("region", { name: "Latest turn in this conversation" })
    expect(within(section).getByRole("status")).toHaveTextContent("Running")
    expect(screen.queryByRole("region", { name: "Shared work dependency graph" })).toBeNull()

    rerender(<NuwaPanel {...props} activity={{ ...activity, status: "cancelled" }} />)
    expect(within(section).getByRole("status")).toHaveTextContent("Stopped")
    fireEvent.click(within(section).getByRole("button", { name: "Files" }))
    expect(onOpenFiles).toHaveBeenCalledOnce()
    fireEvent.click(within(section).getByRole("button", { name: "Open turn Trace" }))
    await waitFor(() => expect(useTraceFocus.getState().turnId).toBe(activity.id))

    rerender(<NuwaPanel {...props} threadId="b" />)
    expect(screen.queryByRole("region", { name: "Latest turn in this conversation" })).toBeNull()
  })

  it("shows reported facts separately from completed work and opens source evidence on demand", async () => {
    render(<NuwaPanel project={project} threadId="a" onSelectAgent={vi.fn()} />)
    const graph = await screen.findByRole("region", { name: "Shared work dependency graph" })
    expect(within(graph).getAllByTestId("nuwa-edge")).toHaveLength(2)
    expect(screen.getByText("chosen-model · high")).toBeInTheDocument()
    fireEvent.click(within(graph).getByRole("button", { name: /Node finding/ }))
    const detail = screen.getByTestId("nuwa-node-detail")
    expect(within(detail).getByText("Reported")).toBeInTheDocument()
    expect(screen.queryByText("Full source evidence")).toBeNull()
    fireEvent.click(within(detail).getByRole("button", { name: "Details and evidence" }))
    expect(await screen.findByText("Full source evidence")).toBeInTheDocument()
    fireEvent.click(within(detail).getByRole("button", { name: /Measured result/ }))
    await waitFor(() => expect(useTraceFocus.getState().turnId).toBe("turn1"))
    expect(useApp.getState().loadUntilTurn).toHaveBeenCalledWith("turn1")
  })

  it("filters a thread without discarding the other thread's prerequisite", async () => {
    render(<NuwaPanel project={project} threadId="b" onSelectAgent={vi.fn()} />)
    await screen.findByRole("region", { name: "Shared work dependency graph" })
    fireEvent.click(screen.getByRole("button", { name: "This conversation" }))
    const graph = screen.getByRole("region", { name: "Shared work dependency graph" })
    expect(within(graph).getAllByRole("button")).toHaveLength(3)
    expect(screen.queryByText("chosen-model · high")).toBeNull()
    fireEvent.click(within(graph).getByRole("button", { name: /Node output/ }))
    expect(screen.getByRole("link", { name: "Open result file" })).toHaveAttribute("href", "/api/threads/b/download/results/report.md")
  })

  it("updates a selected node when a newer project revision arrives", async () => {
    render(<NuwaPanel project={project} threadId="a" onSelectAgent={vi.fn()} />)
    const graph = await screen.findByRole("region", { name: "Shared work dependency graph" })
    fireEvent.click(within(graph).getByRole("button", { name: /Node plan/ }))
    vi.mocked(nuwaAPI.snapshot).mockResolvedValue({ ...snapshot, revision: 2, nodes: snapshot.nodes.map((n) => n.id === "plan" ? { ...n, status: "blocked", summary: "Needs new evidence", version: 2 } : n) })
    act(() => handlers.onRevision(2))
    await waitFor(() => expect(within(screen.getByTestId("nuwa-node-detail")).getByText("Blocked")).toBeInTheDocument())
    expect(within(screen.getByTestId("nuwa-node-detail")).getByText("Needs new evidence")).toBeInTheDocument()
  })

  it("opens the actual worker without inferring its model from its role", async () => {
    const select = vi.fn()
    render(<NuwaPanel project={project} threadId="a" onSelectAgent={select} />)
    const worker = await screen.findByRole("button", { name: /Open agent.*researcher/ })
    expect(worker).toHaveAccessibleName(/chosen-model/)
    expect(worker).toHaveAccessibleName(/high/)
    expect(worker).toHaveAccessibleName(/Running/)
    expect(within(worker).getByText("Parent agent: a / coordinator-1")).toBeInTheDocument()
    fireEvent.click(worker)
    expect(select).toHaveBeenCalledWith("researcher-1")
  })

  it("announces an unrecorded model and the worker status without inferring a model from its role", async () => {
    vi.mocked(nuwaAPI.snapshot).mockResolvedValue({
      ...snapshot,
      workers: snapshot.workers.map((worker) => ({ ...worker, role: "unconfigured-model-role", model_binding: undefined, status: "blocked" })),
    })
    const select = vi.fn()
    render(<NuwaPanel project={project} threadId="a" onSelectAgent={select} />)
    const worker = await screen.findByRole("button", { name: /Open agent.*unconfigured-model-role/ })
    expect(worker).toHaveAccessibleName(/Model not recorded/)
    expect(worker).toHaveAccessibleName(/Blocked/)
    expect(worker).not.toHaveAccessibleName(/chosen-model|high/)
    fireEvent.click(worker)
    expect(select).toHaveBeenCalledWith("researcher-1")
  })

  it("keeps a deleted source explicit instead of offering a broken evidence jump", async () => {
    vi.mocked(nuwaAPI.snapshot).mockResolvedValue({ ...snapshot, nodes: snapshot.nodes.map((n) => n.id === "finding" ? { ...n, unavailable_evidence: n.evidence } : n) })
    render(<NuwaPanel project={project} threadId="a" onSelectAgent={vi.fn()} />)
    const graph = await screen.findByRole("region", { name: "Shared work dependency graph" })
    fireEvent.click(within(graph).getByRole("button", { name: /Node finding/ }))
    fireEvent.click(within(screen.getByTestId("nuwa-node-detail")).getByRole("button", { name: "Details and evidence" }))
    expect(screen.getByRole("button", { name: /Measured result/ })).toBeDisabled()
    expect(screen.getByText("Source unavailable")).toBeInTheDocument()
  })

  it("keeps unknown states visible and exposes every cyclic graph node", () => {
    render(<NuwaGraph nodes={[node("a", { depends_on: ["b"], status: "future-status" }), node("b", { depends_on: ["a"] })]} onSelect={vi.fn()} />)
    expect(screen.getByText("future-status")).toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveTextContent("cycle")
    expect(screen.getAllByRole("button")).toHaveLength(2)
  })

  it("marks a result for review when its dependency has been revised", async () => {
    vi.mocked(nuwaAPI.snapshot).mockResolvedValue({ ...snapshot, nodes: snapshot.nodes.map((n) => n.id === "output" ? { ...n, stale_dependencies: ["finding"], dependency_versions: { finding: 1 } } : n) })
    render(<NuwaPanel project={project} threadId="a" onSelectAgent={vi.fn()} />)
    const graph = await screen.findByRole("region", { name: "Shared work dependency graph" })
    const result = within(graph).getByRole("button", { name: /Node output/ })
    expect(within(result).getByText("Needs review")).toBeInTheDocument()
    fireEvent.click(result)
    expect(within(screen.getByTestId("nuwa-node-detail")).getByText("Source changed; review this result.")).toBeInTheDocument()
    expect(within(screen.getByTestId("nuwa-node-detail")).getByText("Reported")).toBeInTheDocument()
  })

  it("opens an expanded graph with selectable node details", async () => {
    render(<NuwaPanel project={project} threadId="a" onSelectAgent={vi.fn()} />)
    fireEvent.click(await screen.findByRole("button", { name: "Expand graph" }))
    const dialog = screen.getByRole("dialog", { name: "Dependency paths" })
    const graph = within(dialog).getByRole("region", { name: "Shared work dependency graph" })
    fireEvent.click(within(graph).getByRole("button", { name: /Node output/ }))
    expect(within(dialog).getByTestId("nuwa-node-detail")).toContainHTML("Summary output")
    expect(within(graph).getAllByTestId("nuwa-edge")).toHaveLength(2)
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }))
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
  })
})
