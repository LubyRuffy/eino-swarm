import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { beforeEach, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { emptyTranscript } from "@/lib/transcript"
import type { ModelInfo, Thread } from "@/lib/types"
import { useApp } from "@/store/app"
import { WorkerModelRecovery } from "./worker-model-recovery"

vi.mock("@/lib/api", () => ({ api: { recoverWorkerModels: vi.fn() } }))

beforeEach(() => {
  vi.clearAllMocks()
  useApp.setState({ locale: "en", loaded: true, activeId: "thread", meta: undefined,
    threads: [{ id: "thread", provider_id: "chosen", model: "chosen-model" } as Thread],
    models: [{ provider_id: "chosen", model: "chosen-model", ready: true } as ModelInfo],
    status: { running: false },
    transcript: { ...emptyTranscript(), turns: [{ id: "failed", status: "error", userText: "Continue", agentIds: [] }] },
  })
})

it("requires a click, submits the visible selection once, and starts no execution", async () => {
  let resolve!: (value: { configurations: number }) => void
  vi.mocked(api.recoverWorkerModels).mockImplementation(() => new Promise((done) => { resolve = done }))
  render(<WorkerModelRecovery turnId="failed" />)
  const button = screen.getByRole("button", { name: "Repair recovery using chosen-model" })
  expect(api.recoverWorkerModels).not.toHaveBeenCalled()
  fireEvent.click(button)
  expect(button).toBeDisabled()
  fireEvent.click(button)
  expect(api.recoverWorkerModels).toHaveBeenCalledTimes(1)
  expect(api.recoverWorkerModels).toHaveBeenCalledWith("thread", "chosen", "chosen-model")
  await act(async () => resolve({ configurations: 1 }))
  expect(screen.getByRole("status")).toHaveTextContent("You can now Continue or Retry")
  expect(button).toBeDisabled()
})

it("retains a storage refusal and allows another explicit attempt", async () => {
  vi.mocked(api.recoverWorkerModels).mockRejectedValueOnce(new Error("Storage unavailable"))
  render(<WorkerModelRecovery turnId="failed" />)
  fireEvent.click(screen.getByRole("button"))
  await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent("Storage unavailable"))
  expect(screen.getByRole("button")).toBeEnabled()
})

it.each(["running", "restoring", "missing-model"])("refuses repair while %s", (mode) => {
  if (mode === "running") useApp.setState({ status: { running: true } })
  if (mode === "restoring") useApp.setState({ loaded: false })
  if (mode === "missing-model") useApp.setState({ models: [] })
  render(<WorkerModelRecovery turnId="failed" />)
  expect(screen.getByRole("button")).toBeDisabled()
  fireEvent.click(screen.getByRole("button"))
  expect(api.recoverWorkerModels).not.toHaveBeenCalled()
})

it("offers no repair on another historical turn", () => {
  render(<WorkerModelRecovery turnId="older" />)
  expect(screen.queryByRole("button")).toBeNull()
})
