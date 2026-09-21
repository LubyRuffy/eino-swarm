import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { SelectItem } from "@/components/ui/select"

import {
  Field,
  SettingsChoice,
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
    expect(row?.className).toMatch(/\bpy-3\b/)
    expect(screen.getByText("fan-out")).toBeInTheDocument()
    expect(screen.getByText("Sub-agents at once").className).toContain(
      "font-normal",
    )
    expect(screen.getByText("Sub-agents at once").className).toContain(
      "--chrome-font-size",
    )
    expect(screen.getByText("fan-out").className).toContain("--chrome-font-size")
  })

  it("hides a row whose label does not match the query", () => {
    render(
      <Field query="proxy" label="Sub-agents at once">
        <input type="number" />
      </Field>,
    )
    expect(screen.queryByLabelText("Sub-agents at once")).toBeNull()
  })

  it("packs a menu as a compact combobox hugging its value", () => {
    render(
      <SettingsChoice
        label="Appearance"
        hint="chrome only"
        value="system"
        onValueChange={() => undefined}
      >
        <SelectItem value="system">Match the system</SelectItem>
      </SettingsChoice>,
    )
    const trigger = screen.getByRole("combobox", { name: "Appearance" })
    expect(trigger.className).toContain("bg-background")
    expect(trigger.className).toContain("--chrome-font-size")
    expect(trigger.className).toContain("font-normal")
    expect(trigger.className).toMatch(/\bw-auto\b/)
    expect(trigger.className).not.toMatch(/\bw-56\b/)
    expect(trigger.className).not.toMatch(/\bbg-secondary\b/)
    expect(screen.getByText("chrome only")).toBeInTheDocument()
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
    const page = screen.getByRole("heading", { name: "Swarm" }).parentElement
      ?.parentElement
    expect(page?.className).toMatch(/\bgap-8\b/)
    expect(page?.className).toMatch(/\bmx-auto\b/)
    expect(page?.className).toMatch(/\bmax-w-3xl\b/)
  })
})
