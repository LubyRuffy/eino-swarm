import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { MemoryPanel } from "./memory-panel"
import type { Project, ProjectMemory } from "@/lib/types"

vi.mock("@/lib/api", () => ({
  api: { skill: vi.fn() },
}))

const { api } = await import("@/lib/api")

const project: Project = {
  id: "pj_1",
  name: "Anchored",
  system_prompt: "",
  workdir: "",
  resolved_workdir: "/data/projects/pj_1/workspace",
  memory_enabled: true,
  memory_dir: "/data/projects/pj_1/memory",
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
}

const memory: ProjectMemory = {
  dir: "/data/projects/pj_1/memory",
  enabled: true,
  memory: { text: "one note", entries: ["one note"], chars: 8, limit: 2200, rev: "rev1" },
  skills: [{ name: "a-procedure", description: "how to do the thing", updated_at: "" }],
}

function renderPanel(props: Partial<Parameters<typeof MemoryPanel>[0]> = {}) {
  const handlers = {
    onSave: vi.fn().mockResolvedValue(undefined),
    onDeleteSkill: vi.fn(),
    onRefresh: vi.fn(),
    onReview: vi.fn(),
  }
  render(
    <MemoryPanel
      project={project}
      memory={memory}
      loading={false}
      {...handlers}
      {...props}
    />,
  )
  return handlers
}

describe("Memory panel", () => {
  beforeEach(() => vi.clearAllMocks())

  it("shows the notes, what they cost, and the skills", () => {
    renderPanel()
    expect(screen.getByLabelText("Project notes")).toHaveValue("one note")
    expect(screen.getByText("8/2200")).toBeInTheDocument()
    expect(screen.getByText("a-procedure")).toBeInTheDocument()
    expect(screen.getByText("how to do the thing")).toBeInTheDocument()
  })

  // Memory a user cannot correct is memory that repeats its mistake in every
  // future conversation.
  it("saves an edit and then has nothing to save", async () => {
    const { onSave } = renderPanel()
    expect(screen.getByRole("button", { name: "Save notes" })).toBeDisabled()
    fireEvent.change(screen.getByLabelText("Project notes"), {
      target: { value: "a corrected note" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save notes" }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("a corrected note"))
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Save notes" })).toBeDisabled(),
    )
  })

  // A review lands while the panel is open. A textarea that kept its first
  // value would show an empty memory next to a conversation that just filled
  // it, and the user would conclude nothing was remembered.
  it("adopts notes that arrive while it is open", () => {
    const { rerender } = render(
      <MemoryPanel
        project={project}
        memory={memory}
        loading={false}
        onSave={vi.fn()}
        onDeleteSkill={vi.fn()}
        onRefresh={vi.fn()}
        onReview={vi.fn()}
      />,
    )
    rerender(
      <MemoryPanel
        project={project}
        memory={{
          ...memory,
          memory: { ...memory.memory, text: "what the review kept" },
        }}
        loading={false}
        onSave={vi.fn()}
        onDeleteSkill={vi.fn()}
        onRefresh={vi.fn()}
        onReview={vi.fn()}
      />,
    )
    expect(screen.getAllByLabelText("Project notes").at(-1)).toHaveValue(
      "what the review kept",
    )
  })

  // The other half of the same rule: a review must not overwrite what the
  // user is typing.
  it("keeps a half-typed edit when notes arrive underneath it", () => {
    const { rerender } = render(
      <MemoryPanel
        project={project}
        memory={memory}
        loading={false}
        onSave={vi.fn()}
        onDeleteSkill={vi.fn()}
        onRefresh={vi.fn()}
        onReview={vi.fn()}
      />,
    )
    const box = screen.getAllByLabelText("Project notes").at(-1)!
    fireEvent.change(box, { target: { value: "half-typed" } })
    rerender(
      <MemoryPanel
        project={project}
        memory={{
          ...memory,
          memory: { ...memory.memory, text: "what the review kept" },
        }}
        loading={false}
        onSave={vi.fn()}
        onDeleteSkill={vi.fn()}
        onRefresh={vi.fn()}
        onReview={vi.fn()}
      />,
    )
    expect(box).toHaveValue("half-typed")
    expect(
      screen.getByText(/updated while you were editing/),
    ).toBeInTheDocument()
  })

  it("puts back the stored notes when an edit is reverted", () => {
    renderPanel()
    fireEvent.change(screen.getByLabelText("Project notes"), {
      target: { value: "half-typed" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Revert" }))
    expect(screen.getByLabelText("Project notes")).toHaveValue("one note")
  })

  it("reloads the stored notes from a conflict banner", () => {
    const { rerender } = render(
      <MemoryPanel
        project={project}
        memory={memory}
        loading={false}
        onSave={vi.fn()}
        onDeleteSkill={vi.fn()}
        onRefresh={vi.fn()}
        onReview={vi.fn()}
      />,
    )
    fireEvent.change(screen.getByLabelText("Project notes"), {
      target: { value: "half-typed" },
    })
    rerender(
      <MemoryPanel
        project={project}
        memory={{
          ...memory,
          memory: { ...memory.memory, text: "what the review kept", rev: "rev2" },
        }}
        loading={false}
        onSave={vi.fn()}
        onDeleteSkill={vi.fn()}
        onRefresh={vi.fn()}
        onReview={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Reload" }))
    expect(screen.getByLabelText("Project notes")).toHaveValue("what the review kept")
    expect(screen.queryByText(/updated while you were editing/)).not.toBeInTheDocument()
  })

  it("keeps a failed save visible instead of pretending it landed", async () => {
    renderPanel({ onSave: vi.fn().mockRejectedValue(new Error("disk is full")) })
    fireEvent.change(screen.getByLabelText("Project notes"), {
      target: { value: "x" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save notes" }))
    await waitFor(() => expect(screen.getByText("disk is full")).toBeInTheDocument())
    expect(screen.getByRole("button", { name: "Save notes" })).toBeEnabled()
  })

  // The body is not in the list payload, exactly as it is not in the prompt.
  it("fetches a skill's body only when it is opened", async () => {
    vi.mocked(api.skill).mockResolvedValue({
      name: "a-procedure",
      description: "how to do the thing",
      updated_at: "",
      body: "## Steps",
    })
    renderPanel()
    expect(api.skill).not.toHaveBeenCalled()
    fireEvent.click(screen.getByText("a-procedure"))
    await waitFor(() => expect(screen.getByText("Steps")).toBeInTheDocument())
    expect(api.skill).toHaveBeenCalledWith("pj_1", "a-procedure")
  })

  it("opens the named skill when the sidebar sent the user here", async () => {
    vi.mocked(api.skill).mockResolvedValue({
      name: "a-procedure",
      description: "how to do the thing",
      updated_at: "",
      body: "## Steps",
    })
    renderPanel({ focusSkill: "a-procedure" })
    expect(screen.getByRole("button", { expanded: true })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText("Steps")).toBeInTheDocument())
    expect(api.skill).toHaveBeenCalledWith("pj_1", "a-procedure")
  })

  it("asks for a review and a reload on request", () => {
    const { onReview, onRefresh, onDeleteSkill } = renderPanel()
    fireEvent.click(
      screen.getByRole("button", { name: "Review this conversation now" }),
    )
    fireEvent.click(screen.getByRole("button", { name: "Reload memory" }))
    fireEvent.click(
      screen.getByRole("button", { name: "Delete the skill a-procedure" }),
    )
    expect(onReview).toHaveBeenCalled()
    expect(onRefresh).toHaveBeenCalled()
    expect(onDeleteSkill).toHaveBeenCalledWith("a-procedure")
  })

  it("says so when the conversation is not in a project", () => {
    renderPanel({ project: undefined })
    expect(screen.getByText(/not in a project/)).toBeInTheDocument()
    expect(screen.queryByLabelText("Project notes")).not.toBeInTheDocument()
  })

  it("warns that stored memory is not being used when the project has it off", () => {
    renderPanel({ memory: { ...memory, enabled: false } })
    expect(screen.getByText(/switched off for this project/)).toBeInTheDocument()
  })
})
