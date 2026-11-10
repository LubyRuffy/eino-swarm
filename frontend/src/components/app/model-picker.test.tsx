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

  it("keeps the conversation's model on the button when that provider left the list", () => {
    render(
      <ModelPicker
        models={models.filter((row) => row.provider_id === "a")}
        providerId="b"
        model="gamma"
        onChange={vi.fn()}
      />,
    )
    expect(screen.getByRole("button", { name: "Model" })).toHaveTextContent("gamma")
    fireEvent.click(screen.getByLabelText("Model"))
    expect(screen.queryByRole("option", { name: /gamma/ })).toBeNull()
    expect(screen.getByRole("option", { name: /alpha/ })).toBeTruthy()
  })

  it("keeps the stored name when that model left but its provider is still listed", () => {
    render(
      <ModelPicker
        models={models}
        providerId="a"
        model="removed"
        onChange={vi.fn()}
      />,
    )
    expect(screen.getByRole("button", { name: "Model" })).toHaveTextContent("removed")
    fireEvent.click(screen.getByLabelText("Model"))
    expect(screen.queryByRole("option", { name: /removed/ })).toBeNull()
    expect(screen.getByRole("option", { name: /alpha/ })).toBeTruthy()
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

  it("shows a missing settings default as unavailable and does not replace it", () => {
    const onChange = vi.fn()
    const modelsWithGap: ModelInfo[] = [
      { ...models[0], default: false },
      { ...models[1], ready: false, default: true, default_follows_setting: true, model: "removed", label: "removed", id: "a\tremoved" },
    ]
    render(<ModelPicker models={modelsWithGap} providerId="a" model="" onChange={onChange} />)
    const button = screen.getByRole("button", { name: "Model" })
    expect(button).toHaveTextContent("removed")
    expect(button).toHaveAttribute("data-unavailable", "true")
    expect(onChange).not.toHaveBeenCalled()
  })

  it("lists the signed-in server's models before the user's own services", () => {
    const mixed: ModelInfo[] = [
      { id: "g\tserver-a", provider_id: "gateway:x", provider_label: "Server", label: "server-a", model: "server-a", ready: true, default: true, source: "account" },
      ...models.map((m) => ({ ...m, default: false, source: "provider" as const })),
    ]
    render(<ModelPicker models={mixed} providerId="gateway:x" model="server-a" onChange={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "Model" }))
    const headings = Array.from(document.querySelectorAll("[cmdk-group-heading]")).map((n) => n.textContent)
    expect(headings).toEqual(["Server", "One", "Two"])
  })
})
