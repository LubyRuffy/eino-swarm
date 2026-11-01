import { act, fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { PcWebExecutionStatus } from "./pc-web-execution-status"
import { savePcWebExecution } from "@/lib/web-execution"

it("follows the PC by default and distinguishes its account from the phone's preference", () => {
  const onSettings = vi.fn()
  render(<PcWebExecutionStatus connected status={{ preference: "local", execution: "local", cloud_account: "sign_in_required" }} onSettings={onSettings} />)
  expect(screen.getByTestId("pc-web-status")).toHaveTextContent("Next turn Web tools: Follow PC (last refreshed) · Run on this PC")
  act(() => savePcWebExecution("cloud"))
  expect(screen.getByTestId("pc-web-status")).toHaveTextContent("Cloud unavailable · sign in on this PC")
  fireEvent.click(screen.getByRole("button", { name: "Web settings" }))
  expect(onSettings).toHaveBeenCalledOnce()
})

it("shows PC cloud identity as a snapshot rather than a live tool grant", () => {
  render(<PcWebExecutionStatus connected status={{ preference: "auto", execution: "cloud", cloud_account: "signed_in" }} />)
  expect(screen.getByTestId("pc-web-status")).toHaveTextContent("Follow PC (last refreshed) · Cloud server · access checked when used")
})

it("shows unknown or offline instead of inferring cloud availability from a phone login", () => {
  const view = render(<PcWebExecutionStatus connected />)
  expect(screen.getByTestId("pc-web-status")).toHaveTextContent("Unknown · refresh models or update this PC")
  view.rerender(<PcWebExecutionStatus connected={false} status={{ preference: "auto", execution: "cloud", cloud_account: "signed_in" }} />)
  expect(screen.getByTestId("pc-web-status")).toHaveTextContent("PC offline")
  expect(screen.getByTestId("pc-web-status")).not.toHaveTextContent("Cloud server")
})

it("does not promise cloud access when PC account configuration is unavailable", () => {
  render(<PcWebExecutionStatus connected status={{ preference: "cloud", execution: "cloud", cloud_account: "unavailable" }} />)
  expect(screen.getByTestId("pc-web-status")).toHaveTextContent("Cloud unavailable · check this PC's account settings")
})
