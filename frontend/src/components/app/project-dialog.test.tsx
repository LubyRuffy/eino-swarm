import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { ProjectDialog } from "./project-dialog"
import { ToastStack } from "./toast-stack"
import { ApiError } from "@/lib/api"
import type { Project } from "@/lib/types"

const directory = vi.hoisted(() => ({
  available: vi.fn(),
  choose: vi.fn(),
}))
vi.mock("@/lib/workdir", () => ({
  nativeDirectoryAvailable: directory.available,
  chooseDirectory: directory.choose,
}))

beforeEach(() => {
  vi.clearAllMocks()
  directory.available.mockReturnValue(true)
  directory.choose.mockResolvedValue("/picked/project")
})

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
  it("lets a directory selection fill the project without saving it", async () => {
    const { onSave } = renderDialog()
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    await waitFor(() =>
      expect(screen.getByLabelText("Working directory")).toHaveValue(
        "/picked/project",
      ),
    )
    expect(onSave).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Chosen" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Create project" }))
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0][0].workdir).toBe("/picked/project")
  })

  it("waits for directory selection before enabling project creation", async () => {
    let finish!: (path: string) => void
    directory.choose.mockReturnValue(new Promise<string>((resolve) => {
      finish = resolve
    }))
    renderDialog()
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Selected" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    expect(screen.getByRole("button", { name: "Create project" })).toBeDisabled()
    finish("/picked")
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Create project" })).toBeEnabled(),
    )
  })

  it("clears a refused directory error when another directory is selected", async () => {
    const onSave = vi.fn().mockRejectedValue(new ApiError("not a directory", 400, "workdir"))
    renderDialog({ onSave })
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "P" } })
    fireEvent.click(screen.getByRole("button", { name: "Create project" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("not a directory")
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
    expect(screen.getByLabelText("Working directory")).toHaveValue("/picked/project")
  })

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
    // Filesystem names can contain whitespace. Validation belongs to the
    // server; the form must not silently select a different directory.
    expect(onSave.mock.calls[0][0]).toMatchObject({
      name: "Spaced",
      workdir: " /tmp/anchor ",
    })
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  it.each([false, true])("preserves the chosen directory identity when editing=%s", async (editing) => {
    const selected = "/picked/project \t"
    directory.choose.mockResolvedValue(selected)
    const { onSave } = renderDialog(editing ? { project } : {})
    if (!editing) fireEvent.change(screen.getByLabelText("Name"), { target: { value: "Exact" } })
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    await waitFor(() => expect(screen.getByLabelText("Working directory")).toHaveValue(selected))
    fireEvent.click(screen.getByRole("button", { name: editing ? "Save" : "Create project" }))
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0][0].workdir).toBe(selected)
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

  it("toasts a save failure instead of a red line above the buttons", async () => {
    const onSave = vi.fn().mockRejectedValue(new Error("store locked"))
    const onOpenChange = vi.fn()
    render(
      <>
        <ToastStack />
        <ProjectDialog
          open
          memoryAvailable
          onOpenChange={onOpenChange}
          onSave={onSave}
        />
      </>,
    )
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "P" } })
    fireEvent.click(screen.getByRole("button", { name: "Create project" }))
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("Couldn't save the project")
    expect(alert).toHaveTextContent("store locked")
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("alert")).toBeNull()
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
