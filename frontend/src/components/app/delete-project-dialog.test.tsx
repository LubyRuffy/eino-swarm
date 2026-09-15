import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { DeleteProjectDialog } from "./delete-project-dialog"
import type { Project } from "@/lib/types"

const project: Project = {
  id: "pj_1",
  name: "Doomed",
  system_prompt: "",
  workdir: "",
  resolved_workdir: "/data/projects/pj_1/workspace",
  memory_enabled: true,
  memory_dir: "/data/projects/pj_1/memory",
  created_at: "",
  updated_at: "",
}

describe("Delete project dialog", () => {
  // "Delete" does not suggest the conversations go too, so it has to be said.
  it("warns that the conversations and the memory go with it", () => {
    const onConfirm = vi.fn()
    render(
      <DeleteProjectDialog
        project={project}
        onOpenChange={vi.fn()}
        onConfirm={onConfirm}
      />,
    )
    expect(screen.getByText(/conversations and everything it remembered/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "Delete project" }))
    expect(onConfirm).toHaveBeenCalledWith(project)
  })

  // The opposite reassurance matters just as much: a user who pointed a
  // project at their own repository needs to know it survives.
  it("says the user's own directory is left alone", () => {
    render(
      <DeleteProjectDialog
        project={{ ...project, workdir: "/home/me/repo" }}
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    )
    expect(screen.getByText(/working directory are left alone/)).toBeInTheDocument()
  })

  it("stays shut without a project", () => {
    render(<DeleteProjectDialog onOpenChange={vi.fn()} onConfirm={vi.fn()} />)
    expect(
      screen.queryByRole("button", { name: "Delete project" }),
    ).not.toBeInTheDocument()
  })
})
