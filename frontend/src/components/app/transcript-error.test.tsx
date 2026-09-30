import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { defaultAppearance } from "@/lib/appearance"
import { OUTPUT_BUDGET_ERROR } from "@/lib/output-budget"
import type { Settings } from "@/lib/types"
import { useSettingsSheet } from "@/store/settings-sheet"

import { SettingsDialog } from "./settings-dialog"
import { ToastStack } from "./toast-stack"
import { TranscriptError } from "./transcript-error"

vi.mock("@/lib/api", () => ({
  api: {
    settings: vi.fn(),
    tools: vi.fn(),
    saveSettings: vi.fn(),
  },
}))

const { api } = await import("@/lib/api")

const settings: Settings = {
  server: { addr: "127.0.0.1:1", open_browser: false },
  models: { default: "default", providers: [] },
  swarm: {
    max_concurrent: 1,
    agent_timeout_seconds: 1,
    max_turns: 1,
    manager_max_iterations: 1,
    progress_interval_seconds: 1,
    delta_coalesce_ms: 1,
    auto_title: true,
    max_completion_tokens: 16384,
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

function Harness({ text }: { text: string }) {
  const open = useSettingsSheet((s) => s.open)
  const section = useSettingsSheet((s) => s.section)
  const focus = useSettingsSheet((s) => s.focus)
  return (
    <>
      <ToastStack />
      <TranscriptError text={text} />
      <SettingsDialog
        open={open}
        theme="system"
        locale="en"
        appearance={defaultAppearance()}
        onOpenChange={(next) => {
          useSettingsSheet.setState(
            next
              ? { open: true }
              : { open: false, section: "general", focus: "" },
          )
        }}
        onThemeChange={() => undefined}
        onLocaleChange={() => undefined}
        onAppearanceChange={() => undefined}
        onSaved={() => undefined}
        initialSection={section}
        focusKey={focus}
      />
    </>
  )
}

describe("output budget error", () => {
  beforeEach(() => {
    useSettingsSheet.setState({ open: false, section: "general", focus: "" })
    vi.mocked(api.settings).mockResolvedValue(settings)
    vi.mocked(api.tools).mockResolvedValue({ catalog: [], enabled: [] })
  })

  it("links a desktop client to the completion cap", async () => {
    const user = userEvent.setup()
    render(<Harness text={OUTPUT_BUDGET_ERROR} />)
    expect(
      screen.getByText("You can raise the per-request output limit in Settings."),
    ).toBeVisible()
    await user.click(
      screen.getByRole("button", { name: "Max completion tokens" }),
    )
    const cap = await screen.findByLabelText("Max completion (tokens)")
    await waitFor(() => expect(cap).toHaveFocus())
    expect(cap).toHaveValue(16384)
    expect(screen.getByRole("tab", { name: "Swarm" })).toHaveAttribute(
      "aria-selected",
      "true",
    )
  })

  // The landing effect used to depend on the settings object and the search
  // query. The next keystroke saved the sheet and stole the caret back.
  it("lands once, then leaves the caret where the user moved it", async () => {
    const user = userEvent.setup()
    render(<Harness text={OUTPUT_BUDGET_ERROR} />)
    await user.click(
      screen.getByRole("button", { name: "Max completion tokens" }),
    )
    const cap = await screen.findByLabelText("Max completion (tokens)")
    await waitFor(() => expect(cap).toHaveFocus())

    const search = screen.getByRole("textbox", { name: "Search settings" })
    await user.click(search)
    await user.type(search, "out")
    expect(search).toHaveFocus()
    expect(cap).not.toHaveFocus()

    const workers = screen.getByLabelText("Sub-agents at once")
    await user.click(workers)
    fireEvent.change(workers, { target: { value: "2" } })
    expect(workers).toHaveValue(2)
    expect(workers).toHaveFocus()
    expect(cap).not.toHaveFocus()
  })

  it("leaves an ordinary failure as the server text", () => {
    render(<Harness text="the endpoint refused the connection" />)
    expect(screen.getByText("the endpoint refused the connection")).toBeVisible()
    expect(
      screen.queryByRole("button", { name: "Max completion tokens" }),
    ).not.toBeInTheDocument()
    expect(useSettingsSheet.getState().open).toBe(false)
  })
})
