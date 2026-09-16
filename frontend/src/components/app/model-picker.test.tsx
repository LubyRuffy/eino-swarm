import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ModelPicker } from "./model-picker"
import type { ModelInfo } from "@/lib/types"

HTMLElement.prototype.scrollIntoView = function () {}
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal("ResizeObserver", ResizeObserverStub)

const models: ModelInfo[] = [
  {
    id: "a\talpha",
    provider_id: "a",
    provider_label: "One",
    label: "alpha",
    model: "alpha",
    ready: true,
    default: true,
  },
  {
    id: "a\tbeta",
    provider_id: "a",
    provider_label: "One",
    label: "beta",
    model: "beta",
    ready: true,
  },
  {
    id: "b\tgamma",
    provider_id: "b",
    provider_label: "Two",
    label: "gamma",
    model: "gamma",
    ready: true,
  },
]

describe("ModelPicker", () => {
  it("groups by provider, searches, and lets the user pick another", () => {
    const onChange = vi.fn()
    render(
      <ModelPicker
        models={models}
        providerId="a"
        model="alpha"
        onChange={onChange}
      />,
    )
    fireEvent.click(screen.getByLabelText("Model"))
    expect(screen.getByLabelText("Search models")).toBeTruthy()
    expect(screen.getByText("One")).toBeTruthy()
    expect(screen.getByText("Two")).toBeTruthy()
    fireEvent.change(screen.getByLabelText("Search models"), {
      target: { value: "beta" },
    })
    fireEvent.click(screen.getByRole("option", { name: /beta/ }))
    expect(onChange).toHaveBeenCalledWith("a", "beta")
  })

  it("stays a switcher when only one model is ready so refresh and edit remain reachable", () => {
    render(
      <ModelPicker
        models={models.slice(0, 1)}
        providerId="a"
        model="alpha"
        onChange={vi.fn()}
        onRefresh={vi.fn(async () => {})}
        onEdit={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByLabelText("Model"))
    expect(screen.getByRole("option", { name: /alpha/ })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Refresh models" })).toBeTruthy()
    expect(screen.getByRole("button", { name: "Edit providers" })).toBeTruthy()
  })

  it("refreshes catalogs and opens provider settings from the footer", async () => {
    const onRefresh = vi.fn(async () => {})
    const onEdit = vi.fn()
    render(
      <ModelPicker
        models={models}
        providerId="a"
        model="alpha"
        onChange={vi.fn()}
        onRefresh={onRefresh}
        onEdit={onEdit}
      />,
    )
    fireEvent.click(screen.getByLabelText("Model"))
    fireEvent.click(screen.getByRole("button", { name: "Refresh models" }))
    await waitFor(() => expect(onRefresh).toHaveBeenCalled())
    fireEvent.click(screen.getByRole("button", { name: "Edit providers" }))
    expect(onEdit).toHaveBeenCalled()
  })
})
