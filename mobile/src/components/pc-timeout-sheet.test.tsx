import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { PCTimeoutSheet } from "./pc-timeout-sheet"
import { installAndroidBack } from "@/lib/android-back"
import { setLocale } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import type { RemoteResponse, TimeoutSettings } from "@/lib/rpc"

const settings: TimeoutSettings = { agent_timeout_seconds: 600, providers: [{ id: "p", label: "Primary", timeout_seconds: 300, first_byte_timeout_seconds: 30, effective_first_byte_timeout_seconds: 30 }] }
const ok = (value = settings): RemoteResponse => ({ v: 1, id: "r", ok: true, timeout_settings: value })
function link(rpc = vi.fn(async () => ok())): RemoteLink { return { rpc, path: "relay", alive: () => true, close: vi.fn(), announceDevice: vi.fn() } }
beforeEach(() => setLocale("en"))

it("keeps first-byte waiting independent when idle is shorter and saves only the edited field", async () => {
  const rpc = vi.fn().mockResolvedValueOnce(ok()).mockResolvedValueOnce(ok({ ...settings, providers: [{ ...settings.providers[0], timeout_seconds: 20, effective_first_byte_timeout_seconds: 30 }] }))
  render(<PCTimeoutSheet link={link(rpc)} hostLabel="Office PC" onClose={vi.fn()} />)
  const idle = await screen.findByLabelText("Stream idle (seconds) · Primary")
  expect(idle).toHaveClass("text-base")
  expect(screen.getByText("Saved first-byte wait: 30s.")).toBeInTheDocument()
  fireEvent.change(idle, { target: { value: "20" } })
  expect(screen.getByText("First-byte wait after save: 30s.")).toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "Save PC timeouts" }))
  expect(await screen.findByText("PC timeouts saved.")).toBeInTheDocument()
  expect(rpc.mock.calls[1][0]).toEqual({ op: "set_timeouts", timeout_settings: { providers: [{ id: "p", timeout_seconds: 20 }] } })
  expect(screen.getByText("Saved first-byte wait: 30s.")).toBeInTheDocument()
  expect(screen.getByRole("button", { name: "Save PC timeouts" })).toBeDisabled()
})

it("invalid fields do not show a stale preview or enable Save", async () => {
  render(<PCTimeoutSheet link={link()} hostLabel="Office PC" onClose={vi.fn()} />)
  const first = await screen.findByLabelText("First byte (seconds) · Primary")
  fireEvent.change(first, { target: { value: "0" } })
  expect(screen.getByRole("alert")).toHaveTextContent("Use positive whole seconds.")
  expect(screen.getByText("Enter positive whole seconds to preview the first-byte wait.")).toBeInTheDocument()
  expect(screen.queryByText("Saved first-byte wait: 30s.")).not.toBeInTheDocument()
  expect(screen.getByRole("button", { name: "Save PC timeouts" })).toBeDisabled()
})

it("owns Android Back and restores the underlying hook after closing", async () => {
  const base = vi.fn(() => true), close = vi.fn()
  const restore = installAndroidBack(base)
  const view = render(<PCTimeoutSheet link={link()} hostLabel="Office PC" onClose={close} />)
  await screen.findByLabelText("Sub-agent lifetime (seconds)")
  act(() => { expect(window.__zwaiAndroidBack?.()).toBe(true) })
  expect(close).toHaveBeenCalledOnce(); expect(base).not.toHaveBeenCalled()
  view.unmount()
  expect(window.__zwaiAndroidBack?.()).toBe(true); expect(base).toHaveBeenCalledOnce()
  restore()
})

it("allows Close and Escape while a save is pending, without claiming cancellation", async () => {
  let resolve!: (r: RemoteResponse) => void
  const pending = new Promise<RemoteResponse>((yes) => { resolve = yes })
  const rpc = vi.fn().mockResolvedValueOnce(ok()).mockReturnValueOnce(pending)
  const close = vi.fn(); const view = render(<PCTimeoutSheet link={link(rpc)} hostLabel="Office PC" onClose={close} />)
  fireEvent.change(await screen.findByLabelText("Sub-agent lifetime (seconds)"), { target: { value: "3600" } })
  fireEvent.click(screen.getByRole("button", { name: "Save PC timeouts" }))
  expect(screen.getByRole("button", { name: "Saving…" })).toBeDisabled()
  expect(screen.getByLabelText("Sub-agent lifetime (seconds)")).toBeDisabled()
  fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Close" }))
  fireEvent.keyDown(window, { key: "Escape" }); expect(close).toHaveBeenCalledTimes(2)
  expect(screen.queryByText(/cancelled/i)).not.toBeInTheDocument()
  view.unmount(); await act(async () => { resolve(ok()); await pending })
  expect(rpc).toHaveBeenCalledTimes(2)
})

it("keeps a legacy host refusal local and does not render editable fallback defaults", async () => {
  const target = link(vi.fn(async () => ({ v: 1, id: "r", ok: false, code: "unknown_op", error: "secret upstream details" })))
  render(<PCTimeoutSheet link={target} hostLabel="Office PC" onClose={vi.fn()} />)
  expect(await screen.findByRole("alert")).toHaveTextContent("Update the PC app to edit its timeouts.")
  expect(screen.queryByLabelText("Sub-agent lifetime (seconds)")).not.toBeInTheDocument()
  expect(screen.queryByText(/secret/)).not.toBeInTheDocument()
  expect(screen.getByRole("button", { name: "Save PC timeouts" })).toBeDisabled()
})

it("offers an explicit load retry and keeps worker settings usable for an empty catalog", async () => {
  const rpc = vi.fn().mockRejectedValueOnce(new Error("private path")).mockResolvedValueOnce(ok({ ...settings, providers: [] }))
  render(<PCTimeoutSheet link={link(rpc)} hostLabel="Office PC" onClose={vi.fn()} />)
  fireEvent.click(await screen.findByRole("button", { name: "Retry" }))
  await waitFor(() => expect(screen.getByLabelText("Sub-agent lifetime (seconds)")).toHaveValue(600))
  expect(screen.getByText("This PC has no configured model providers.")).toBeInTheDocument()
})

// A slower model's initial response can need longer than a paused stream.
it("previews and saves a first-byte wait longer than stream idle", async () => {
  const saved = { ...settings, providers: [{ ...settings.providers[0], first_byte_timeout_seconds: 900, effective_first_byte_timeout_seconds: 900 }] }
  const rpc = vi.fn().mockResolvedValueOnce(ok()).mockResolvedValueOnce(ok(saved))
  render(<PCTimeoutSheet link={link(rpc)} hostLabel="Office PC" onClose={vi.fn()} />)
  fireEvent.change(await screen.findByLabelText("First byte (seconds) · Primary"), { target: { value: "900" } })
  expect(screen.getByText("First-byte wait after save: 900s.")).toBeInTheDocument()
  fireEvent.click(screen.getByRole("button", { name: "Save PC timeouts" }))
  expect(await screen.findByText("PC timeouts saved.")).toBeInTheDocument()
  expect(rpc.mock.calls[1][0]).toEqual({ op: "set_timeouts", timeout_settings: { providers: [{ id: "p", first_byte_timeout_seconds: 900 }] } })
  expect(screen.getByText("Saved first-byte wait: 900s.")).toBeInTheDocument()
})
