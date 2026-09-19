import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { SettingsDialog } from "./settings-dialog"
import type { Settings } from "@/lib/types"

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
vi.stubGlobal("ResizeObserver", ResizeObserverStub)

vi.mock("@/lib/api", () => ({
  api: {
    settings: vi.fn(),
    tools: vi.fn(),
    saveSettings: vi.fn(),
    remoteStatus: vi.fn(),
    remoteOffer: vi.fn(),
    saveRemoteToken: vi.fn(),
    remoteBindings: vi.fn(),
    revokeRemoteBinding: vi.fn(),
  },
}))

const { api } = await import("@/lib/api")

const base: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: {
    default: "default",
    providers: [
      {
        id: "default",
        label: "Endpoint",
        base_url: "http://endpoint.invalid/v1",
        model: "",
        catalog: [],
        timeout_seconds: 300,
        has_api_key: false,
        ready: false,
      },
    ],
  },
  swarm: {
    max_concurrent: 1,
    agent_timeout_seconds: 1,
    max_turns: 1,
    manager_max_iterations: 1,
    progress_interval_seconds: 1,
    delta_coalesce_ms: 1,
    auto_title: true,
  },
  tools: {
    disabled: [],
    enabled: [],
    proxy: { http: "", https: "", no_proxy: "" },
    web_search_max_results: 8,
  },
  memory: {
    enabled: true,
    auto_review: true,
    char_limit: 1,
    entry_max: 1,
    review_max_iterations: 1,
    skills_index_max: 1,
    notifications: "on",
  },
  log: { level: "info" },
}

function renderDialog(props: Partial<Parameters<typeof SettingsDialog>[0]> = {}) {
  render(
    <SettingsDialog
      open
      theme="system"
      locale="system"
      appearance={{ font: "system", fontSize: "medium", contentWidth: "comfortable" }}
      onOpenChange={vi.fn()}
      onThemeChange={vi.fn()}
      onLocaleChange={vi.fn()}
      onAppearanceChange={vi.fn()}
      onSaved={vi.fn()}
      {...props}
    />,
  )
}

function activePanel() {
  return document.querySelector('[role="tabpanel"][data-state="active"]')
}

describe("Settings dialog", () => {
  beforeEach(() => {
    vi.mocked(api.settings).mockResolvedValue(base)
    vi.mocked(api.tools).mockResolvedValue({ catalog: [], enabled: [] })
    vi.mocked(api.saveSettings).mockReset()
    vi.mocked(api.saveSettings).mockImplementation(async (patch) => ({
      ...base,
      ...patch,
    }))
    vi.mocked(api.remoteStatus).mockResolvedValue({
      enabled: false,
      hub_url: "",
      has_token: false,
      online: false,
    })
    vi.mocked(api.remoteBindings).mockResolvedValue([])
  })

  // The Add-a-provider outline sits on the last pixel of the Models
  // scrollport. overflow-y-auto leaves TabsContent's overflow-hidden in
  // place, and with no bottom padding the 1px border is clipped.
  // Swarm used to skip the scrollport, so Save left the window.
  it("lets every tab scroll inside a full-page sheet", async () => {
    renderDialog()
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )

    expect(screen.getByRole("dialog").className).toMatch(/\bh-dvh\b/)
    expect(screen.getByRole("button", { name: "Back to app" })).toBeInTheDocument()
    // modal={false}: Radix otherwise walks the conversation to aria-hide it.
    expect(document.querySelector(".bg-black\\/60")).toBeNull()
    expect(screen.queryByTestId("settings-titlebar")).toBeNull()
    expect(screen.getByLabelText("Search settings")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull()
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull()
    expect(api.saveSettings).not.toHaveBeenCalled()

    for (const name of [
      "Models",
      "Swarm",
      "Tools",
      "Memory",
      "Personality",
      "General",
      "Phone",
    ] as const) {
      fireEvent.click(screen.getByRole("tab", { name }))
      const panel = activePanel()
      expect(panel?.className).toMatch(/\boverflow-auto\b/)
      expect(panel?.className).toMatch(/\bpb-1\b/)
      expect(panel?.className).toMatch(/\bflex-1\b/)
      expect(panel?.className).not.toMatch(/\boverflow-hidden\b/)
      expect(panel?.className).not.toMatch(/\boverflow-y-auto\b/)
    }
  })

  it("jumps to Swarm when search matches a swarm field", async () => {
    const user = userEvent.setup()
    renderDialog()
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    await user.type(screen.getByLabelText("Search settings"), "coalesce")
    await waitFor(() =>
      expect(screen.getByRole("tab", { name: "Swarm" })).toHaveAttribute(
        "data-state",
        "active",
      ),
    )
    expect(screen.queryByRole("tab", { name: "Models" })).toBeNull()
    expect(screen.getByLabelText("Stream coalesce (ms)")).toBeInTheDocument()
  })

  it("exposes compact settings on the Swarm tab", async () => {
    const user = userEvent.setup()
    renderDialog()
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    await user.click(screen.getByRole("tab", { name: "Swarm" }))
    await waitFor(() =>
      expect(
        screen.getByLabelText("Context budget (characters)"),
      ).toBeInTheDocument(),
    )
    expect(
      screen.getByLabelText("Messages to keep when compacting"),
    ).toBeInTheDocument()
    expect(screen.getByLabelText("Auto-compact at (tokens)")).toBeInTheDocument()
    expect(
      screen.queryByLabelText("Compact timeout (seconds)"),
    ).not.toBeInTheDocument()
    expect(
      screen.getByLabelText("Goal auto-continue turns"),
    ).toBeInTheDocument()
  })

  // Full-page Settings covers the app header. On the macOS desktop window
  // the traffic lights still occupy the first 48px, so Back to app has to
  // start under an empty drag strip — the Codex layout, not a padded label.
  it("keeps Back to app below the native title bar on desktop", async () => {
    renderDialog({ trafficInset: true })
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )

    const titlebar = screen.getByTestId("settings-titlebar")
    const back = screen.getByRole("button", { name: "Back to app" })
    expect(titlebar).toHaveAttribute("data-drag-region")
    expect(titlebar).toHaveClass("h-12")
    expect(titlebar).not.toContainElement(back)
    expect(
      titlebar.compareDocumentPosition(back) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  // Codex/Cursor write on the toggle; a Save/Cancel pair implies a draft
  // that the rest of this sheet no longer has.
  it("writes a memory toggle without a Save button", async () => {
    const user = userEvent.setup()
    renderDialog()
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    await user.click(screen.getByRole("tab", { name: "Memory" }))
    const toggle = await screen.findByRole("switch", {
      name: "Remember anything at all",
    })
    expect(toggle).toBeChecked()
    expect(
      screen.getByLabelText("Per-note cap (characters)"),
    ).toBeInTheDocument()
    await user.click(toggle)
    await waitFor(() => expect(api.saveSettings).toHaveBeenCalled())
    expect(vi.mocked(api.saveSettings).mock.calls[0][0].memory?.enabled).toBe(
      false,
    )
    expect(vi.mocked(api.saveSettings).mock.calls[0][0].ui).toEqual({
      locale: "system",
      font: "system",
      font_size: "medium",
      content_width: "comfortable",
    })
  })

  it("writes personality without a Save button", async () => {
    const user = userEvent.setup()
    renderDialog()
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    await user.click(screen.getByRole("tab", { name: "Personality" }))
    const box = await screen.findByLabelText("Personal preferences")
    fireEvent.change(box, { target: { value: "prefer compact replies" } })
    await waitFor(() => expect(api.saveSettings).toHaveBeenCalled())
    expect(
      vi.mocked(api.saveSettings).mock.calls.at(-1)?.[0].personality
        ?.instructions,
    ).toBe("prefer compact replies")
  })

  it("writes the chrome language with the rest of the document", async () => {
    const user = userEvent.setup()
    renderDialog({ locale: "zh" })
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    await user.click(screen.getByRole("tab", { name: "Memory" }))
    await user.click(
      await screen.findByRole("switch", { name: "Remember anything at all" }),
    )
    await waitFor(() => expect(api.saveSettings).toHaveBeenCalled())
    const patch = vi.mocked(api.saveSettings).mock.calls.at(-1)?.[0]
    expect(patch?.memory?.enabled).toBe(false)
    expect(patch?.ui).toEqual({
      locale: "zh",
      font: "system",
      font_size: "medium",
      content_width: "comfortable",
    })
  })

  it("flushes a pending edit when leaving", async () => {
    const onOpenChange = vi.fn()
    const onSaved = vi.fn()
    renderDialog({ onOpenChange, onSaved })
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole("tab", { name: "Swarm" }))
    fireEvent.change(await screen.findByLabelText("Sub-agents at once"), {
      target: { value: "3" },
    })
    await user.click(screen.getByRole("button", { name: "Back to app" }))
    await waitFor(() => expect(api.saveSettings).toHaveBeenCalled())
    expect(
      vi.mocked(api.saveSettings).mock.calls.at(-1)?.[0].swarm?.max_concurrent,
    ).toBe(3)
    expect(onSaved).toHaveBeenCalled()
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it("keeps the sheet open and shows the error when a write fails", async () => {
    vi.mocked(api.saveSettings).mockRejectedValue(new Error("disk full"))
    const onOpenChange = vi.fn()
    renderDialog({ onOpenChange })
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    const user = userEvent.setup()
    await user.click(screen.getByRole("tab", { name: "Memory" }))
    await user.click(
      await screen.findByRole("switch", { name: "Remember anything at all" }),
    )
    fireEvent.click(screen.getByRole("button", { name: "Back to app" }))
    await waitFor(() =>
      expect(screen.getByText("disk full")).toBeInTheDocument(),
    )
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    expect(screen.getByRole("dialog")).toBeInTheDocument()
  })

  it("offers font, size and conversation width on General", async () => {
    const user = userEvent.setup()
    renderDialog()
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: "Add a provider" }),
      ).toBeInTheDocument(),
    )
    await user.click(screen.getByRole("tab", { name: "General" }))
    expect(screen.getByLabelText("Font")).toHaveTextContent("System")
    expect(screen.getByLabelText("Font size")).toHaveTextContent("Medium")
    expect(screen.getByLabelText("Conversation width")).toHaveTextContent(
      "Standard",
    )
  })
})
