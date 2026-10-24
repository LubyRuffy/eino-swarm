import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { GoalBanner } from "./goal-banner"

describe("goal editing during engine restoration", () => {
  it("keeps a focused edit through restoration and blocks Enter and blur until readiness", () => {
    const onEdit = vi.fn().mockResolvedValue(true)
    const onClear = vi.fn().mockResolvedValue(true)
    const onResume = vi.fn()
    const { rerender } = render(<GoalBanner goal="Existing objective" capped onEdit={onEdit} onClear={onClear} onResume={onResume} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    const box = screen.getByTestId("goal-edit")
    fireEvent.change(box, { target: { value: "Retained objective edit" } })
    expect(box).toHaveFocus()
    rerender(<GoalBanner goal="Existing objective" capped disabled onEdit={onEdit} onClear={onClear} onResume={onResume} />)
    fireEvent.keyDown(box, { key: "Enter" })
    fireEvent.blur(box)
    expect(onEdit).not.toHaveBeenCalled()
    expect(screen.getByTestId("goal-edit")).toHaveValue("Retained objective edit")
    expect(screen.getByTestId("goal-edit")).toBeDisabled()
    expect(screen.getByRole("button", { name: "Start goal" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Clear goal" })).toBeDisabled()
    rerender(<GoalBanner goal="Existing objective" capped onEdit={onEdit} onClear={onClear} onResume={onResume} />)
    expect(screen.getByTestId("goal-edit")).toBeEnabled()
    expect(onEdit).not.toHaveBeenCalled()
  })

  it("retains the edit after a refusal and closes only after an explicit acknowledged retry", async () => {
    const onEdit = vi.fn().mockResolvedValueOnce(false).mockResolvedValueOnce(true)
    render(<GoalBanner goal="Existing objective" onEdit={onEdit} onClear={vi.fn()} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    fireEvent.change(screen.getByTestId("goal-edit"), { target: { value: "Retained objective edit" } })
    fireEvent.keyDown(screen.getByTestId("goal-edit"), { key: "Enter" })
    await waitFor(() => expect(onEdit).toHaveBeenCalledTimes(1))
    expect(screen.getByTestId("goal-edit")).toHaveValue("Retained objective edit")
    fireEvent.keyDown(screen.getByTestId("goal-edit"), { key: "Enter" })
    await waitFor(() => expect(screen.queryByTestId("goal-edit")).toBeNull())
    expect(onEdit).toHaveBeenCalledTimes(2)
  })

  it("holds the edit while its acknowledgement is pending and excludes duplicate saves", async () => {
    let acknowledge!: (ok: boolean) => void
    const onEdit = vi.fn(() => new Promise<boolean>((resolve) => { acknowledge = resolve }))
    render(<GoalBanner goal="Existing objective" onEdit={onEdit} onClear={vi.fn()} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    const box = screen.getByTestId("goal-edit")
    fireEvent.change(box, { target: { value: "Pending objective edit" } })
    fireEvent.keyDown(box, { key: "Enter" })
    fireEvent.blur(box)
    expect(onEdit).toHaveBeenCalledTimes(1)
    expect(screen.getByTestId("goal-edit")).toBeDisabled()
    await act(async () => { acknowledge(true) })
    expect(screen.queryByTestId("goal-edit")).toBeNull()
  })

  it("keeps an empty edit if clearing the goal is refused", async () => {
    const onClear = vi.fn().mockResolvedValue(false)
    render(<GoalBanner goal="Existing objective" onEdit={vi.fn()} onClear={onClear} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    fireEvent.change(screen.getByTestId("goal-edit"), { target: { value: "" } })
    fireEvent.blur(screen.getByTestId("goal-edit"))
    await waitFor(() => expect(onClear).toHaveBeenCalledTimes(1))
    expect(screen.getByTestId("goal-edit")).toHaveValue("")
  })

  it("retains a rejected edit and closes an unchanged edit without a mutation", async () => {
    const onEdit = vi.fn().mockRejectedValue(new Error("lost acknowledgement"))
    render(<GoalBanner goal="Existing objective" onEdit={onEdit} onClear={vi.fn()} />)
    fireEvent.click(screen.getByTestId("goal-text"))
    fireEvent.change(screen.getByTestId("goal-edit"), { target: { value: "Rejected edit" } })
    await act(async () => { fireEvent.blur(screen.getByTestId("goal-edit")) })
    expect(screen.getByTestId("goal-edit")).toHaveValue("Rejected edit")
    fireEvent.change(screen.getByTestId("goal-edit"), { target: { value: "Existing objective" } })
    fireEvent.blur(screen.getByTestId("goal-edit"))
    expect(screen.queryByTestId("goal-edit")).toBeNull()
    expect(onEdit).toHaveBeenCalledTimes(1)
  })
})
