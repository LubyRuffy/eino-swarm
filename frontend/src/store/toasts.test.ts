import { afterEach, describe, expect, it } from "vitest"

import { resetToasts, useToasts } from "./toasts"

afterEach(() => {
  resetToasts()
})

describe("toasts", () => {
  it("replaces a toast with the same id instead of stacking a twin", () => {
    useToasts.getState().push({
      id: "discover:dgx",
      kind: "error",
      title: "Couldn't list models",
      message: "first",
    })
    useToasts.getState().push({
      id: "discover:dgx",
      kind: "error",
      title: "Couldn't list models",
      message: "second",
    })
    expect(useToasts.getState().toasts).toEqual([
      {
        id: "discover:dgx",
        kind: "error",
        title: "Couldn't list models",
        message: "second",
      },
    ])
  })

  it("keeps later cards when the stack is full", () => {
    for (let i = 0; i < 6; i++) {
      useToasts.getState().push({
        kind: "error",
        message: `err-${i}`,
      })
    }
    expect(useToasts.getState().toasts.map((t) => t.message)).toEqual([
      "err-2",
      "err-3",
      "err-4",
      "err-5",
    ])
  })

  it("drops a card on dismiss and clears the rest", () => {
    const id = useToasts.getState().push({ kind: "error", message: "keep" })
    useToasts.getState().push({ id: "gone", kind: "error", message: "drop" })
    useToasts.getState().dismiss("gone")
    expect(useToasts.getState().toasts.map((t) => t.id)).toEqual([id])
    useToasts.getState().clear()
    expect(useToasts.getState().toasts).toEqual([])
  })
})
