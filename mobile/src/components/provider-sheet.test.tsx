import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ProviderSheet } from "./provider-sheet"
import { installAndroidBack } from "@/lib/android-back"
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

  it("saves an endpoint switched off so the composer can skip it", () => {
    setLocale("en")
    const onChange = vi.fn()
    render(<ProviderSheet providers={[]} onChange={onChange} onClose={vi.fn()} discover={vi.fn()} />)
    fireEvent.change(screen.getByLabelText(t("chat.baseUrl")), {
      target: { value: "https://endpoint.invalid/v1" },
    })
    fireEvent.click(screen.getByRole("checkbox", { name: t("chat.listed") }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.save") }))
    const saved = onChange.mock.calls[0][0] as DirectProvider[]
    expect(saved[0].enabled).toBe(false)
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
    fireEvent.click(screen.getByRole("button", { name: /^Desk/ }))
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
    fireEvent.click(screen.getByRole("button", { name: /^Desk/ }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.discover") }))
    fireEvent.change(screen.getByLabelText(t("chat.name")), { target: { value: "Renamed" } })
    finish(["alpha"])
    await waitFor(() => expect(screen.getByRole("option", { name: "alpha" })).toBeInTheDocument())
    expect(screen.getByLabelText(t("chat.name"))).toHaveValue("Renamed")
  })

  it("removes one saved endpoint after confirmation and keeps the others", () => {
    setLocale("en")
    localStorage.setItem("zwai.phone.providers", "stale")
    const onChange = vi.fn()
    const onClose = vi.fn()
    const managed = draft({ id: "gateway:acct", label: "Server", sourceKind: "gateway" })
    const drop = draft({ id: "p-drop", label: "Drop" })
    const keep = draft({ id: "p-keep", label: "Keep" })
    const view = render(
      <ProviderSheet
        providers={[managed, drop, keep]}
        onChange={onChange}
        onClose={onClose}
        discover={vi.fn()}
      />,
    )
    expect(screen.queryByRole("button", { name: t("chat.removeName", { name: "Server" }) })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: t("chat.removeName", { name: "Drop" }) }))
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: t("chat.removeCancel") }))
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: t("chat.removeName", { name: "Keep" }) }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.removeName", { name: "Drop" }) }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.removeConfirm") }))
    expect(onClose).not.toHaveBeenCalled()
    const saved = onChange.mock.calls[0][0] as DirectProvider[]
    expect(saved.map((row) => row.id)).toEqual(["p-keep"])
    expect(localStorage.getItem("zwai.phone.providers")).toContain("p-keep")
    expect(localStorage.getItem("zwai.phone.providers")).not.toContain("p-drop")
    expect(localStorage.getItem("zwai.phone.providers")).not.toContain("gateway:")
    view.rerender(
      <ProviderSheet
        providers={[managed, ...saved]}
        onChange={onChange}
        onClose={onClose}
        discover={vi.fn()}
      />,
    )
    expect(screen.getByText("Server")).toBeInTheDocument()
    expect(screen.getByText("Keep")).toBeInTheDocument()
    expect(screen.queryByText("Drop")).not.toBeInTheDocument()
  })

  it("removes the last personal endpoint from the edit form without deleting history or an account model", () => {
    setLocale("en")
    const onChange = vi.fn()
    const onClose = vi.fn()
    localStorage.setItem("zwai.phone.directThreads", "{\"kept\":true}")
    const managed = draft({ id: "gateway:acct", label: "Server", sourceKind: "gateway" })
    const only = draft({ id: "p-only", label: "Only" })
    const view = render(
      <ProviderSheet providers={[managed, only]} onChange={onChange} onClose={onClose} discover={vi.fn()} />,
    )
    fireEvent.click(screen.getByRole("button", { name: /^Only/ }))
    fireEvent.click(screen.getByRole("button", { name: t("chat.remove") }))
    expect(onChange).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: t("chat.removeConfirm") }))
    expect(onClose).not.toHaveBeenCalled()
    expect(onChange.mock.calls[0][0]).toEqual([])
    expect(localStorage.getItem("zwai.phone.providers")).toBeNull()
    expect(localStorage.getItem("zwai.phone.directThreads")).toBe("{\"kept\":true}")
    view.rerender(
      <ProviderSheet providers={[managed]} onChange={onChange} onClose={onClose} discover={vi.fn()} />,
    )
    expect(screen.getByRole("heading", { name: t("chat.models") })).toBeInTheDocument()
    expect(screen.getByText("Server")).toBeInTheDocument()
    expect(screen.queryByLabelText(t("chat.baseUrl"))).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: t("chat.removeName", { name: "Server" }) })).not.toBeInTheDocument()
  })

  it("lets Android Back cancel a pending removal", () => {
    setLocale("en")
    const base = vi.fn(() => true)
    const restore = installAndroidBack(base)
    const onChange = vi.fn()
    const view = render(
      <ProviderSheet
        providers={[draft({ id: "p-1", label: "Desk" })]}
        onChange={onChange}
        onClose={vi.fn()}
        discover={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: t("chat.removeName", { name: "Desk" }) }))
    act(() => {
      expect(window.__zwaiAndroidBack?.()).toBe(true)
    })
    expect(base).not.toHaveBeenCalled()
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.queryByRole("button", { name: t("chat.removeConfirm") })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: t("chat.removeName", { name: "Desk" }) })).toBeInTheDocument()
    act(() => {
      expect(window.__zwaiAndroidBack?.()).toBe(true)
    })
    expect(base).toHaveBeenCalledOnce()
    view.unmount()
    restore()
  })
})
