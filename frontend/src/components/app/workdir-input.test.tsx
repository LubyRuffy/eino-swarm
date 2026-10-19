import { useState } from "react"
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { WorkdirInput } from "./workdir-input"
import { Label } from "@/components/ui/label"

const directory = vi.hoisted(() => ({
  available: vi.fn(),
  choose: vi.fn(),
}))
vi.mock("@/lib/workdir", () => ({
  nativeDirectoryAvailable: directory.available,
  chooseDirectory: directory.choose,
}))

function deferred() {
  let resolve!: (path: string) => void
  let reject!: (error: Error) => void
  const promise = new Promise<string>((done, fail) => {
    resolve = done
    reject = fail
  })
  return { promise, resolve, reject }
}

function renderInput(props: Partial<Parameters<typeof WorkdirInput>[0]> = {}) {
  const onChange = vi.fn()
  const onBusyChange = vi.fn()
  function Field() {
    const [value, setValue] = useState(props.value ?? "/existing")
    return (
      <>
        <Label htmlFor="project-workdir">Working directory</Label>
        <WorkdirInput
          value={value}
          onChange={(next) => {
            setValue(next)
            onChange(next)
          }}
          onBusyChange={onBusyChange}
          {...props}
        />
      </>
    )
  }
  return { ...render(<Field />), onChange, onBusyChange }
}

beforeEach(() => {
  vi.clearAllMocks()
  directory.available.mockReturnValue(true)
  directory.choose.mockResolvedValue("/chosen")
})

describe("Working directory input", () => {
  it("opens the native directory picker from the current path", async () => {
    const { onChange } = renderInput()
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    await waitFor(() =>
      expect(screen.getByLabelText("Working directory")).toHaveValue("/chosen"),
    )
    expect(directory.choose).toHaveBeenCalledWith({
      title: "Working directory",
      buttonText: "Choose directory",
      directory: "/existing",
    })
    expect(onChange).toHaveBeenCalledOnce()
  })

  it("keeps the existing path when the picker is cancelled", async () => {
    directory.choose.mockResolvedValue("")
    const { onChange } = renderInput()
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Choose directory" })).toBeEnabled(),
    )
    expect(screen.getByLabelText("Working directory")).toHaveValue("/existing")
    expect(onChange).not.toHaveBeenCalled()
  })

  it("shows picker errors against the input and clears them on a new value", async () => {
    directory.choose.mockRejectedValue(new Error("picker unavailable"))
    renderInput()
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("picker unavailable")
    expect(screen.getByLabelText("Working directory")).toHaveAttribute("aria-invalid", "true")
    fireEvent.change(screen.getByLabelText("Working directory"), {
      target: { value: "/manual" },
    })
    expect(screen.queryByRole("alert")).toBeNull()
    expect(screen.getByLabelText("Working directory")).toHaveAttribute("aria-invalid", "false")
  })

  it("marks the picker busy and prevents another selection until it finishes", async () => {
    const pending = deferred()
    directory.choose.mockReturnValue(pending.promise)
    const { onBusyChange } = renderInput()
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    expect(screen.getByRole("button", { name: "Choosing directory…" })).toBeDisabled()
    expect(onBusyChange).toHaveBeenLastCalledWith(true)
    await act(async () => pending.resolve("/chosen"))
    expect(screen.getByRole("button", { name: "Choose directory" })).toBeEnabled()
    expect(onBusyChange).toHaveBeenLastCalledWith(false)
  })

  it("offers manual entry without unusable native controls in a browser", () => {
    directory.available.mockReturnValue(false)
    const { onChange } = renderInput()
    expect(screen.queryByRole("button", { name: "Choose directory" })).toBeNull()
    expect(screen.getByText("Choose a directory in the desktop app.")).toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Working directory"), {
      target: { value: "/manual" },
    })
    expect(onChange).toHaveBeenCalledWith("/manual")
  })

  it("disables changes while the project is saving", () => {
    renderInput({ disabled: true })
    expect(screen.getByLabelText("Working directory")).toBeDisabled()
    expect(screen.getByRole("button", { name: "Choose directory" })).toBeDisabled()
  })

  it("preserves a manual edit made while the native picker was open", async () => {
    const pending = deferred()
    directory.choose.mockReturnValue(pending.promise)
    renderInput()
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    fireEvent.change(screen.getByLabelText("Working directory"), {
      target: { value: "/manual" },
    })
    await act(async () => pending.resolve("/stale"))
    expect(screen.getByLabelText("Working directory")).toHaveValue("/manual")
  })

  it.each(["success", "error"])("ignores an old picker %s after the input was closed and reopened", async (result) => {
    const pending = deferred()
    directory.choose.mockReturnValue(pending.promise)
    const onChange = vi.fn()
    const { rerender } = render(<WorkdirInput active value="/original" onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    rerender(<WorkdirInput active={false} value="/original" onChange={onChange} />)
    rerender(<WorkdirInput active value="/reopened" onChange={onChange} />)
    await act(async () => {
      if (result === "success") pending.resolve("/stale")
      else pending.reject(new Error("old picker failed"))
    })
    expect(screen.getByRole("textbox")).toHaveValue("/reopened")
    expect(screen.queryByRole("alert")).toBeNull()
    expect(onChange).not.toHaveBeenCalled()
  })

  it("ignores picker results after unmount", async () => {
    const pending = deferred()
    directory.choose.mockReturnValue(pending.promise)
    const { unmount, onChange, onBusyChange } = renderInput()
    fireEvent.click(screen.getByRole("button", { name: "Choose directory" }))
    unmount()
    await act(async () => pending.resolve("/stale"))
    expect(onChange).not.toHaveBeenCalled()
    expect(onBusyChange).toHaveBeenLastCalledWith(false)
  })
})
