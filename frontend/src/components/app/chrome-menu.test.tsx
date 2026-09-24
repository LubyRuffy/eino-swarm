import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ChromeMenu } from "./chrome-menu"
import { useApp } from "@/store/app"

describe("ChromeMenu", () => {
  it("opens theme, language and the build version from the bottom-left control", () => {
    useApp.setState({
      theme: "light",
      meta: {
        version: "v0.1.11",
        mode: "desktop",
        mock: false,
        configured: true,
        default_provider: "",
        reasoning_levels: [],
        data_dir: "/tmp",
        capabilities: {},
        swarm: {
          max_concurrent: 1,
          agent_timeout_seconds: 1,
          max_turns: 1,
          manager_max_iterations: 1,
          progress_interval_seconds: 1,
          delta_coalesce_ms: 1,
          auto_title: false,
        },
      },
    })
    const onToggleTheme = vi.fn()
    const onToggleLocale = vi.fn()
    render(
      <ChromeMenu onToggleTheme={onToggleTheme} onToggleLocale={onToggleLocale} />,
    )
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Switch theme" }))
    expect(onToggleTheme).toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    fireEvent.click(screen.getByRole("menuitem", { name: "Switch language" }))
    expect(onToggleLocale).toHaveBeenCalled()
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    expect(screen.getByTestId("app-version")).toHaveTextContent("Version v0.1.11")
  })

  it("omits the version row until the engine has reported one", () => {
    useApp.setState({ meta: undefined })
    render(<ChromeMenu onToggleTheme={vi.fn()} onToggleLocale={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    expect(screen.queryByTestId("app-version")).not.toBeInTheDocument()
  })

  it("flips conversation width and developer view from the app menu", () => {
    useApp.setState({ contentWidth: "comfortable", transcriptMode: "user" })
    render(<ChromeMenu onToggleTheme={vi.fn()} onToggleLocale={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    const wide = screen.getByRole("menuitem", { name: "Switch to wide layout" })
    expect(wide).toHaveAttribute("aria-pressed", "false")
    fireEvent.click(wide)
    expect(useApp.getState().contentWidth).toBe("full")
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    const dev = screen.getByRole("menuitem", { name: "Switch to developer view" })
    expect(dev).toHaveAttribute("aria-pressed", "false")
    fireEvent.click(dev)
    expect(useApp.getState().transcriptMode).toBe("developer")
  })

  it("offers the way back while the column is wide and developer view is on", () => {
    useApp.setState({ contentWidth: "full", transcriptMode: "developer" })
    render(<ChromeMenu onToggleTheme={vi.fn()} onToggleLocale={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    expect(screen.getByRole("menuitem", { name: "Switch to standard layout" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
    expect(screen.getByRole("menuitem", { name: "Switch to user view" })).toHaveAttribute(
      "aria-pressed",
      "true",
    )
  })

  it("names the other shells only when more than one is connected", () => {
    useApp.setState({ meta: metaWith([{ id: "pc_a", surface: "desktop" }]) })
    render(<ChromeMenu onToggleTheme={vi.fn()} onToggleLocale={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    expect(screen.queryByTestId("shared-clients")).not.toBeInTheDocument()
  })

  it("lists each connected surface inside the app menu", () => {
    useApp.setState({
      meta: metaWith([
        { id: "pc_a", surface: "desktop" },
        { id: "pc_b", surface: "web" },
      ]),
    })
    render(<ChromeMenu onToggleTheme={vi.fn()} onToggleLocale={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "App menu" }))
    expect(screen.getByTestId("shared-clients")).toHaveTextContent(
      "Also open in Desktop · Browser",
    )
  })
})

function metaWith(clients: { id: string; surface: string }[]) {
  return {
    version: "",
    mode: "engine" as const,
    mock: false,
    configured: true,
    default_provider: "",
    reasoning_levels: [],
    data_dir: "",
    capabilities: {},
    swarm: {
      max_concurrent: 1,
      agent_timeout_seconds: 1,
      max_turns: 1,
      manager_max_iterations: 1,
      progress_interval_seconds: 1,
      delta_coalesce_ms: 1,
      auto_title: false,
    },
    clients,
  }
}
