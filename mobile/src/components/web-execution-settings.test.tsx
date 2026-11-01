import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, expect, it, vi } from "vitest"
import { WebExecutionSettings } from "./web-execution-settings"
import { saveGatewaySession, type GatewaySession } from "@/lib/gateway-account"
import { setLocale } from "@/lib/i18n"

const session: GatewaySession = { origin: "https://account.invalid", token: "agm_" + "d".repeat(64), user: { id: "owner", username: "owner", enabled: true, approval_status: "approved" }, device: { id: "device", expires_at: "2099-01-01T00:00:00Z" } }
beforeEach(() => { localStorage.clear(); setLocale("en"); saveGatewaySession(session) })
afterEach(() => vi.restoreAllMocks())

it("separates phone Web tools from the PC policy and never offers phone-local execution", () => {
  render(<WebExecutionSettings />)
  const direct = screen.getByLabelText("Phone chat Web tools")
  const pc = screen.getByLabelText("PC conversation Web tools")
  expect(direct).toHaveValue("auto")
  expect(pc).toHaveValue("inherit")
  expect(Array.from((direct as HTMLSelectElement).options, option => option.value)).toEqual(["auto", "cloud", "off"])
  expect(Array.from((pc as HTMLSelectElement).options, option => option.value)).toEqual(["inherit", "local", "cloud"])
  fireEvent.change(direct, { target: { value: "off" } })
  expect(direct).toHaveValue("off")
  expect(pc).toHaveValue("inherit")
  fireEvent.change(pc, { target: { value: "local" } })
  expect(direct).toHaveValue("off")
  expect(pc).toHaveValue("local")
  expect(screen.getByText(/independently of the model source/)).toBeVisible()
  expect(screen.getByText(/PC's own signed-in account/)).toBeVisible()
})

it("retains the two independent preferences after reopening settings", () => {
  const view = render(<WebExecutionSettings />)
  fireEvent.change(screen.getByLabelText("Phone chat Web tools"), { target: { value: "off" } })
  fireEvent.change(screen.getByLabelText("PC conversation Web tools"), { target: { value: "cloud" } })
  view.unmount()
  render(<WebExecutionSettings />)
  expect(screen.getByLabelText("Phone chat Web tools")).toHaveValue("off")
  expect(screen.getByLabelText("PC conversation Web tools")).toHaveValue("cloud")
})

it("does not carry one account's location into another account", () => {
  render(<WebExecutionSettings />)
  fireEvent.change(screen.getByLabelText("Phone chat Web tools"), { target: { value: "off" } })
  fireEvent.change(screen.getByLabelText("PC conversation Web tools"), { target: { value: "local" } })
  act(() => saveGatewaySession({ ...session, user: { ...session.user, id: "another" } }))
  expect(screen.getByLabelText("Phone chat Web tools")).toHaveValue("auto")
  expect(screen.getByLabelText("PC conversation Web tools")).toHaveValue("inherit")
  act(() => saveGatewaySession(session))
  expect(screen.getByLabelText("Phone chat Web tools")).toHaveValue("off")
  expect(screen.getByLabelText("PC conversation Web tools")).toHaveValue("local")
})

it("reports a failed durable preference save and retains the previous selection", () => {
  render(<WebExecutionSettings />)
  vi.spyOn(localStorage, "setItem").mockImplementation(() => { throw new DOMException("quota", "QuotaExceededError") })
  fireEvent.change(screen.getByLabelText("Phone chat Web tools"), { target: { value: "off" } })
  expect(screen.getByRole("alert")).toHaveTextContent("Could not save")
  expect(screen.getByLabelText("Phone chat Web tools")).toHaveValue("auto")
  expect(screen.getByLabelText("PC conversation Web tools")).toHaveValue("inherit")
})
