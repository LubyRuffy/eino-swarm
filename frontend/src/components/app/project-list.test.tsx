import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ProjectList } from "./project-list"
import type { Project } from "@/lib/types"

const projects: Project[] = [
  {
    id: "pj_1",
    name: "First",
    system_prompt: "",
    workdir: "",
    resolved_workdir: "/data/projects/pj_1/workspace",
    memory_enabled: true,
    memory_dir: "/data/projects/pj_1/memory",
    created_at: "",
    updated_at: "",
  },
]

function renderList(props: Partial<Parameters<typeof ProjectList>[0]> = {}) {
  const handlers = {
    onSelect: vi.fn(),
    onNew: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
    onOpenSkill: vi.fn(),
  }
  render(<ProjectList projects={projects} {...handlers} {...props} />)
  return handlers
}

describe("Project list", () => {
  it("selects a project and clears the filter again", () => {
    const { onSelect } = renderList({ selectedId: "pj_1" })
    expect(screen.getByRole("button", { name: "First" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
    fireEvent.click(screen.getByRole("button", { name: "All conversations" }))
    expect(onSelect).toHaveBeenCalledWith(undefined)
  })

  it("offers a new project and a menu on each row", () => {
    const { onNew } = renderList()
    fireEvent.click(screen.getByRole("button", { name: "New project" }))
    expect(onNew).toHaveBeenCalled()
    // Every row's menu is named after its project: two projects would
    // otherwise give the same nameless trigger twice, to a screen reader and
    // to the E2E suite alike.
    expect(
      screen.getByRole("button", { name: "Project options for First" }),
    ).toBeInTheDocument()
  })

  // With nothing in the list, the section has to say what a project is for;
  // an empty heading tells nobody anything.
  it("explains what a project is when there are none", () => {
    renderList({ projects: [] })
    expect(screen.getByText(/one working directory/)).toBeInTheDocument()
  })

  it("lists skills under the project that recorded them", () => {
    const { onOpenSkill } = renderList({
      projects: [
        {
          ...projects[0],
          skills: [{ name: "a-procedure", description: "when it applies", updated_at: "" }],
        },
      ],
    })
    expect(screen.getByTestId("project-skills")).toBeInTheDocument()
    // The count is visual; putting it in the accessible name would make
    // getByRole("button", { name: "First" }) miss after the first skill.
    expect(screen.getByRole("button", { name: "First" })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Open skill a-procedure" }))
    expect(onOpenSkill).toHaveBeenCalledWith(
      expect.objectContaining({ id: "pj_1" }),
      expect.objectContaining({ name: "a-procedure" }),
    )
  })

  it("does not invent an empty skills list under a project that has none", () => {
    renderList()
    expect(screen.queryByTestId("project-skills")).not.toBeInTheDocument()
  })

  it("caps the sidebar list and points at Memory for the rest", () => {
    const { onOpenSkill } = renderList({
      projects: [
        {
          ...projects[0],
          skills: Array.from({ length: 9 }, (_, i) => ({
            name: `skill-${i + 1}`,
            description: "",
            updated_at: "",
          })),
        },
      ],
    })
    expect(screen.getByRole("button", { name: "Open skill skill-1" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Open skill skill-8" })).toBeInTheDocument()
    expect(
      screen.queryByRole("button", { name: "Open skill skill-9" }),
    ).not.toBeInTheDocument()
    fireEvent.click(
      screen.getByRole("button", { name: "Show remaining skills for First" }),
    )
    expect(onOpenSkill).toHaveBeenCalledWith(expect.objectContaining({ id: "pj_1" }))
    expect(onOpenSkill.mock.calls[0][1]).toBeUndefined()
  })
})
