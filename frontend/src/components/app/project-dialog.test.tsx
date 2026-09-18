import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ProjectDialog } from "./project-dialog"
import { ApiError } from "@/lib/api"
import type { Project } from "@/lib/types"

const project: Project = {
  id: "pj_1",
  name: "Existing",
  system_prompt: "carried into every turn",
  workdir: "/somewhere",
  resolved_workdir: "/somewhere",
  memory_enabled: true,
  memory_dir: "/data/projects/pj_1/memory",
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
}

function renderDialog(props: Partial<Parameters<typeof ProjectDialog>[0]> = {}) {
  const onSave = vi.fn().mockResolvedValue(project)
  const onOpenChange = vi.fn()
  render(
    <ProjectDialog
      open
      memoryAvailable
      onOpenChange={onOpenChange}
      onSave={onSave}
      {...props}
    />,
  )
  return { onSave, onOpenChange }
}

describe("Project dialog", () => {
  it("sends what the user typed and closes", async () => {
    const { onSave, onOpenChange } = renderDialog()
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "  Spaced  " },
    })
    fireEvent.change(screen.getByLabelText("Working directory"), {
      target: { value: " /tmp/anchor " },
    })
    fireEvent.click(screen.getByRole("button", { name: "Create project" }))

    await waitFor(() => expect(onSave).toHaveBeenCalled())
    // Trailing whitespace in a path is a typo, never an intention, and the
    // server would refuse it.
    expect(onSave.mock.calls[0][0]).toMatchObject({
      name: "Spaced",
      workdir: "/tmp/anchor",
    })
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  it("cannot create a nameless project", () => {
    renderDialog()
    expect(screen.getByRole("button", { name: "Create project" })).toBeDisabled()
  })

  // An empty instruction used to be a blinking caret in blank space.
  it("draws a border around the instruction so it reads as a field", () => {
    renderDialog()
    expect(screen.getByLabelText("Instruction").className).toMatch(
      /\bborder-input\b/,
    )
  })

  // A refused directory is the one failure a user can actually fix, so it is
  // shown against the field rather than as a banner they have to map back.
  it("shows a refused working directory under its field and stays open", async () => {
    const onSave = vi
      .fn()
      .mockRejectedValue(new ApiError("that is not a directory", 400, "workdir"))
    const onOpenChange = vi.fn()
    render(
      <ProjectDialog
        open
        memoryAvailable
        onOpenChange={onOpenChange}
        onSave={onSave}
      />,
    )
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "P" } })
    fireEvent.click(screen.getByRole("button", { name: "Create project" }))

    await waitFor(() =>
      expect(screen.getByText("that is not a directory")).toBeInTheDocument(),
    )
    expect(screen.getByLabelText("Working directory")).toHaveAttribute(
      "aria-invalid",
      "true",
    )
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it("loads the project being edited", () => {
    renderDialog({ project })
    expect(screen.getByLabelText("Name")).toHaveValue("Existing")
    expect(screen.getByLabelText("Instruction")).toHaveValue(
      "carried into every turn",
    )
    expect(screen.getByText(/pj_1\/memory/)).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled()
  })

  // Offering a switch that the install-wide setting overrides would promise
  // something the app will not do.
  it("disables the memory switch when memory is off for the install", () => {
    renderDialog({ memoryAvailable: false })
    expect(screen.getByLabelText("Memory")).toBeDisabled()
    expect(screen.getByLabelText("Memory")).not.toBeChecked()
  })
})
