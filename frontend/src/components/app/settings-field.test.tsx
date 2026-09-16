import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import {
  Field,
  SettingsPage,
  SettingsSection,
  settingsMatch,
} from "./settings-field"

describe("settingsMatch", () => {
  it("is true when the query is empty", () => {
    expect(settingsMatch("", "Timeout")).toBe(true)
    expect(settingsMatch("   ", "Timeout")).toBe(true)
  })

  it("matches any haystack, case-insensitively", () => {
    expect(settingsMatch("time", "Sub-agent timeout (seconds)")).toBe(true)
    expect(settingsMatch("API", "api key", "stored")).toBe(true)
    expect(settingsMatch("proxy", "HTTP")).toBe(false)
  })
})

describe("settings field row", () => {
  it("puts the label on the left of the control and wires htmlFor", () => {
    render(
      <Field label="Sub-agents at once" hint="fan-out">
        <input type="number" />
      </Field>,
    )
    const input = screen.getByLabelText("Sub-agents at once")
    const row = input.closest("div")?.parentElement
    expect(row?.className).toMatch(/\bjustify-between\b/)
    expect(screen.getByText("fan-out")).toBeInTheDocument()
  })

  it("hides a row whose label does not match the query", () => {
    render(
      <Field query="proxy" label="Sub-agents at once">
        <input type="number" />
      </Field>,
    )
    expect(screen.queryByLabelText("Sub-agents at once")).toBeNull()
  })

  it("omits a section given no children, and renders a titled card when a row remains", () => {
    const { rerender } = render(
      <SettingsSection title="Runtime">{null}</SettingsSection>,
    )
    expect(screen.queryByText("Runtime")).toBeNull()

    rerender(
      <SettingsPage title="Swarm" description="How workers run.">
        <SettingsSection title="Runtime">
          <Field label="Sub-agents at once">
            <input type="number" />
          </Field>
        </SettingsSection>
      </SettingsPage>,
    )
    expect(screen.getByRole("heading", { name: "Swarm" })).toBeInTheDocument()
    expect(screen.getByText("How workers run.")).toBeInTheDocument()
    expect(screen.getByText("Runtime")).toBeInTheDocument()
  })
})
