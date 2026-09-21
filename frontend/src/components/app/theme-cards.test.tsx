import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { AppearanceTheme } from "./theme-cards"

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal("ResizeObserver", ResizeObserverStub)

describe("AppearanceTheme", () => {
  it("picks light, dark, and system from the preview cards", () => {
    const onThemeChange = vi.fn()
    render(
      <AppearanceTheme
        theme="system"
        onThemeChange={onThemeChange}
        palette="zwai"
        onPaletteChange={vi.fn()}
      />,
    )
    expect(screen.getByRole("radio", { name: "Match the system" })).toHaveAttribute(
      "aria-checked",
      "true",
    )
    fireEvent.click(screen.getByRole("radio", { name: "Light" }))
    expect(onThemeChange).toHaveBeenCalledWith("light")
    fireEvent.click(screen.getByRole("radio", { name: "Dark" }))
    expect(onThemeChange).toHaveBeenCalledWith("dark")
  })

  it("picks FOFA from the color theme menu", () => {
    const onPaletteChange = vi.fn()
    render(
      <AppearanceTheme
        theme="dark"
        onThemeChange={vi.fn()}
        palette="zwai"
        onPaletteChange={onPaletteChange}
      />,
    )
    fireEvent.click(screen.getByRole("combobox", { name: "Color theme" }))
    fireEvent.click(screen.getByRole("option", { name: "FOFA" }))
    expect(onPaletteChange).toHaveBeenCalledWith("fofa")
  })

  it("restyles the cards when the named set changes", () => {
    const { rerender } = render(
      <AppearanceTheme
        theme="light"
        onThemeChange={vi.fn()}
        palette="zwai"
        onPaletteChange={vi.fn()}
      />,
    )
    expect(document.querySelector('[data-swatch="zwai-light"]')).toBeTruthy()
    rerender(
      <AppearanceTheme
        theme="light"
        onThemeChange={vi.fn()}
        palette="fofa"
        onPaletteChange={vi.fn()}
      />,
    )
    expect(document.querySelector('[data-swatch="fofa-light"]')).toBeTruthy()
  })

  it("keeps Color theme visible when searching for theme", () => {
    render(
      <AppearanceTheme
        query="theme"
        theme="system"
        onThemeChange={vi.fn()}
        palette="zwai"
        onPaletteChange={vi.fn()}
      />,
    )
    expect(screen.getByRole("radiogroup", { name: "Theme" })).toBeTruthy()
    expect(screen.getByRole("combobox", { name: "Color theme" })).toBeTruthy()
  })

  it("hides the cards when the query is about fonts", () => {
    render(
      <AppearanceTheme
        query="serif"
        theme="system"
        onThemeChange={vi.fn()}
        palette="zwai"
        onPaletteChange={vi.fn()}
      />,
    )
    expect(screen.queryByRole("radiogroup", { name: "Theme" })).toBeNull()
    expect(
      screen.queryByRole("combobox", { name: "Color theme" }),
    ).toBeNull()
  })
})
