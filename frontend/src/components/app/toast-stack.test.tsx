import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"

import { ToastStack } from "./toast-stack"
import { resetToasts, useToasts } from "@/store/toasts"

afterEach(() => {
  act(() => {
    resetToasts()
  })
})

describe("ToastStack", () => {
  it("paints a dismissible error over the page, not inside a scrollport", () => {
    render(<ToastStack />)
    act(() => {
      useToasts.getState().push({
        id: "discover:default",
        kind: "error",
        title: "Couldn't list models",
        message: "provider: list models: connect: no route to host",
      })
    })
    const alert = screen.getByRole("alert")
    expect(alert).toHaveTextContent("Couldn't list models")
    expect(alert).toHaveTextContent("connect: no route to host")
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }))
    expect(screen.queryByRole("alert")).toBeNull()
    expect(useToasts.getState().toasts).toEqual([])
  })

  it("still shows the body when the toast has no title", () => {
    render(<ToastStack />)
    act(() => {
      useToasts.getState().push({
        kind: "error",
        message: "provider: list models: timed out",
      })
    })
    expect(screen.getByRole("alert")).toHaveTextContent(
      "provider: list models: timed out",
    )
  })
})
