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
    onTidySkills: vi.fn(),
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
    expect(screen.queryByRole("button", { name: "Save notes" })).not.toBeInTheDocument()
  })

  // Memory a user cannot correct is memory that repeats its mistake in every
  // future conversation. Save is not a fixture of the pane: it appears for
  // an edit and leaves once there is nothing left to write.
  it("saves an edit and then has nothing to save", async () => {
    const { onSave } = renderPanel()
    expect(screen.queryByRole("button", { name: "Save notes" })).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Project notes"), {
      target: { value: "a corrected note" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save notes" }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith("a corrected note"))
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Save notes" })).not.toBeInTheDocument(),
    )
  })

  it("hides Save again when the draft matches what is stored", () => {
    renderPanel()
    fireEvent.change(screen.getByLabelText("Project notes"), {
      target: { value: "a corrected note" },
    })
    expect(screen.getByRole("button", { name: "Save notes" })).toBeEnabled()
    fireEvent.change(screen.getByLabelText("Project notes"), {
      target: { value: "one note" },
    })
    expect(screen.queryByRole("button", { name: "Save notes" })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Revert" })).not.toBeInTheDocument()
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
        onTidySkills={vi.fn()}
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
        onTidySkills={vi.fn()}
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
        onTidySkills={vi.fn()}
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
        onTidySkills={vi.fn()}
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
    expect(screen.queryByRole("button", { name: "Save notes" })).not.toBeInTheDocument()
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
        onTidySkills={vi.fn()}
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
        onTidySkills={vi.fn()}
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
    const { onReview, onRefresh, onDeleteSkill, onTidySkills } = renderPanel()
    fireEvent.click(
      screen.getByRole("button", { name: "Review this conversation now" }),
    )
    fireEvent.click(screen.getByRole("button", { name: "Reload memory" }))
    fireEvent.click(screen.getByRole("button", { name: "Tidy skills" }))
    fireEvent.click(
      screen.getByRole("button", { name: "Delete the skill a-procedure" }),
    )
    expect(onDeleteSkill).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Delete skill" }))
    expect(onReview).toHaveBeenCalled()
    expect(onRefresh).toHaveBeenCalled()
    expect(onTidySkills).toHaveBeenCalled()
    expect(onDeleteSkill).toHaveBeenCalledWith("a-procedure")
  })

  it("spins and says so while skills are being tidied", () => {
    renderPanel({ tidying: true })
    expect(screen.getByTestId("tidy-status")).toHaveTextContent(/Asking the model/)
    expect(screen.getByText(/Scanning 1 skills/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Tidy skills" })).toBeDisabled()
  })

  it("reports a tidy that folded nothing", () => {
    renderPanel({
      tidyReport: {
        scanned: 1,
        before: 1,
        after: 1,
        families: 0,
        unchanged: 1,
        created: [],
        deleted: [],
        patched: [],
        merged: [],
        reviewed: true,
      },
    })
    expect(screen.getByTestId("tidy-status")).toHaveTextContent(/nothing to merge/i)
    expect(screen.getByTestId("tidy-stats")).toHaveTextContent(/1 scanned/)
  })

  it("reports a tidy that folded overlapping skills", () => {
    renderPanel({
      tidyReport: {
        scanned: 2,
        before: 2,
        after: 1,
        families: 1,
        unchanged: 0,
        created: ["a-procedure"],
        deleted: ["a-procedure-notes"],
        patched: [],
        merged: [
          { keep: "a-procedure", dropped: ["a-procedure-notes"], created: true },
        ],
        reviewed: true,
      },
    })
    expect(screen.getByTestId("tidy-status")).toHaveTextContent(/Skills curated/)
    expect(screen.getByText("a-procedure-notes → a-procedure")).toBeInTheDocument()
  })

  it("keeps a failed tidy visible instead of pretending it landed", () => {
    renderPanel({ tidyError: "cannot tidy skills" })
    expect(screen.getByTestId("tidy-status")).toHaveTextContent("cannot tidy skills")
  })

  it("marks the tidy control when the catalog still has a family", () => {
    renderPanel({ memory: { ...memory, needs_tidy: true } })
    expect(screen.getByRole("button", { name: "Tidy skills" })).toHaveClass(
      "border",
    )
  })

  it("spins and says so while a review is in flight", () => {
    renderPanel({ reviewing: true })
    expect(screen.getByTestId("review-status")).toHaveTextContent(/last finished turn/)
    expect(
      screen.getByRole("button", { name: "Review this conversation now" }),
    ).toBeDisabled()
  })

  it("reports a review that kept nothing", () => {
    renderPanel({ reviewHint: "Review finished — nothing new to keep." })
    expect(screen.getByTestId("review-status")).toHaveTextContent(/nothing new to keep/)
  })

  it("caps the notes box so Skills are not pushed off the pane", () => {
    renderPanel()
    expect(screen.getByLabelText("Project notes")).toHaveClass("max-h-36")
    expect(screen.getByTestId("skills-list")).toHaveClass("overflow-auto")
  })

  it("keeps an opened skill's body inside its card instead of overlaying the list", async () => {
    vi.mocked(api.skill).mockResolvedValue({
      name: "a-procedure",
      description: "how to do the thing",
      updated_at: "",
      body: "## Steps\n\n`/data/unbroken-token-that-used-to-widen-the-pane`",
    })
    renderPanel({
      memory: {
        ...memory,
        skills: [
          { name: "a-procedure", description: "how to do the thing", updated_at: "" },
          { name: "another-procedure", description: "a later one", updated_at: "" },
        ],
      },
    })
    fireEvent.click(screen.getByText("a-procedure"))
    const body = await screen.findByTestId("skill-body")
    expect(body).toHaveClass("overflow-auto")
    expect(body).toHaveClass("min-w-0")
    expect(body).toHaveClass("break-words")
    expect(body).toHaveClass("max-h-72")
    const card = body.closest("[data-testid=skill-card]")
    expect(card).toHaveClass("overflow-hidden")
    expect(card).toHaveClass("min-w-0")
    expect(card?.contains(body)).toBe(true)
    // Still in the list, after this card — not a floating layer on top of it.
    const sibling = screen.getByText("another-procedure")
    expect(
      body.compareDocumentPosition(sibling) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it("shows the whole skill description instead of clipping it", () => {
    renderPanel({
      memory: {
        ...memory,
        skills: [
          {
            name: "a-procedure",
            description:
              "when the query is slow, decide whether the store or the client is the cause",
            updated_at: "",
          },
        ],
      },
    })
    const description = screen.getByText(/when the query is slow/)
    expect(description).toHaveTextContent(
      "when the query is slow, decide whether the store or the client is the cause",
    )
    expect(description.className).not.toMatch(/\btruncate\b/)
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
