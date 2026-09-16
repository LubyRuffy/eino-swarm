import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ConfirmDeleteDialog } from "./confirm-delete-dialog"

describe("ConfirmDeleteDialog", () => {
  it("does not delete until the confirm button is pressed", () => {
    const onConfirm = vi.fn()
    const onOpenChange = vi.fn()
    render(
      <ConfirmDeleteDialog
        open
        title="Delete Alpha?"
        description="The transcript is removed."
        confirmLabel="Delete conversation"
        cancelLabel="Cancel"
        onOpenChange={onOpenChange}
        onConfirm={onConfirm}
      />,
    )
    expect(onConfirm).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }))
    expect(onConfirm).not.toHaveBeenCalled()
    expect(onOpenChange).toHaveBeenCalledWith(false)

    fireEvent.click(screen.getByRole("button", { name: "Delete conversation" }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it("stays out of the document when closed", () => {
    render(
      <ConfirmDeleteDialog
        open={false}
        title="Delete Alpha?"
        description="gone"
        confirmLabel="Delete conversation"
        cancelLabel="Cancel"
        onOpenChange={vi.fn()}
        onConfirm={vi.fn()}
      />,
    )
    expect(
      screen.queryByRole("button", { name: "Delete conversation" }),
    ).not.toBeInTheDocument()
  })
})
