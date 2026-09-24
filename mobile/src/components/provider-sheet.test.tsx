import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ProviderSheet } from "./provider-sheet"
import { blankProvider, type DirectProvider } from "@/lib/direct-provider"
import { setLocale, t } from "@/lib/i18n"

function draft(patch: Partial<DirectProvider> = {}): DirectProvider {
  return {
    ...blankProvider(),
    id: "p-1",
    label: "Desk",
    baseURL: "https://endpoint.invalid/v1",
    model: "one",
    ...patch,
  }
}

describe("ProviderSheet", () => {
  it("stays inside the screen when the form is open", () => {
    setLocale("en")
    render(<ProviderSheet providers={[]} onChange={vi.fn()} onClose={vi.fn()} discover={vi.fn()} />)
    const sheet = screen.getByTestId("provider-sheet")
    expect(sheet).toHaveClass("w-full", "max-w-full", "min-w-0", "overflow-x-hidden")
    expect(sheet.parentElement).toHaveClass("overflow-x-hidden", "max-w-full")
    expect(screen.getByLabelText(t("chat.baseUrl"))).toHaveClass("text-base")
    expect(screen.getByLabelText(t("chat.api"))).toHaveClass("text-base")
  })

  it("saves an endpoint and the wire the user picked", () => {
    setLocale("en")
    const onChange = vi.fn()
    const onClose = vi.fn()
    render(<ProviderSheet providers={[]} onChange={onChange} onClose={onClose} discover={vi.fn()} />)
    fireEvent.change(screen.getByLabelText(t("chat.name")), { target: { value: "Desk" } })
    fireEvent.change(screen.getByLabelText(t("chat.baseUrl")), {
      target: { value: "https://endpoint.invalid/v1" },
    })
    fireEvent.change(screen.getByLabelText(t("chat.api")), { target: { value: "responses" } })
    fireEvent.change(screen.getByLabelText(t("chat.defaultModel")), { target: { value: "one" } })
    fireEvent.click(screen.getByRole("button", { name: t("chat.save") }))
    expect(onClose).toHaveBeenCalled()
    const saved = onChange.mock.calls[0][0] as DirectProvider[]
    expect(saved[0]).toMatchObject({
      label: "Desk",
      baseURL: "https://endpoint.invalid/v1",
      api: "responses",
      model: "one",
    })
  })

  it("refuses a base URL that is not http(s)", () => {
    setLocale("en")
    const onChange = vi.fn()
    render(
      <ProviderSheet providers={[]} onChange={onChange} onClose={vi.fn()} discover={vi.fn()} />,
    )
    fireEvent.change(screen.getByLabelText(t("chat.baseUrl")), { target: { value: "javascript:nope" } })
    fireEvent.click(screen.getByRole("button", { name: t("chat.save") }))
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole("alert")).toHaveTextContent(t("chat.badUrl"))
  })

  it("fills the model list from discover", async () => {
    setLocale("en")
    const discover = vi.fn(async () => ["alpha", "beta"])
    render(
      <ProviderSheet
        providers={[draft({ model: "", catalog: [] })]}
        onChange={vi.fn()}
        onClose={vi.fn()}
        discover={discover}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /Desk/ }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.discover") }))
    await waitFor(() => expect(screen.getByRole("option", { name: "alpha" })).toBeInTheDocument())
    expect(discover).toHaveBeenCalled()
  })

  it("keeps a name typed while discover is still running", async () => {
    setLocale("en")
    let finish: (names: string[]) => void = () => undefined
    const discover = vi.fn(
      () =>
        new Promise<string[]>((resolve) => {
          finish = resolve
        }),
    )
    render(
      <ProviderSheet
        providers={[draft({ model: "", catalog: [] })]}
        onChange={vi.fn()}
        onClose={vi.fn()}
        discover={discover}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /Desk/ }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.discover") }))
    fireEvent.change(screen.getByLabelText(t("chat.name")), { target: { value: "Renamed" } })
    finish(["alpha"])
    await waitFor(() => expect(screen.getByRole("option", { name: "alpha" })).toBeInTheDocument())
    expect(screen.getByLabelText(t("chat.name"))).toHaveValue("Renamed")
  })
})
