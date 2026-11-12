import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { ApiError, api } from "@/lib/api"
import { useProjects } from "@/store/projects"
import { useToasts } from "@/store/toasts"

import { BackupTab } from "./settings-backup"

vi.mock("@/lib/backup-file", () => ({
  nativeBackupFileAvailable: () => false,
  chooseBackupFile: vi.fn(),
}))

describe("backup settings", () => {
  beforeEach(() => {
    vi.stubGlobal("URL", { createObjectURL: () => "blob:backup", revokeObjectURL: () => {} })
    useProjects.setState({ projects: [{ id: "pj_one", name: "One" } as never], selectedId: "pj_one" })
    vi.spyOn(api, "backupDownload").mockResolvedValue({ blob: new Blob(["PK"]), skippedSymlinks: 0 })
    vi.spyOn(api, "backupPreviewFile").mockResolvedValue({
      manifest: { kind: "project", includes_secrets: false, counts: { projects: 1, threads: 2 }, skipped_symlinks: 2 },
      conflicts: [],
      action: "add-project",
    })
    vi.spyOn(api, "backupRestoreFile").mockResolvedValue({ kind: "project" })
    vi.spyOn(api, "backupProgress").mockResolvedValue({ running: false, done: 0, total: 0 })
    vi.spyOn(api, "meta").mockResolvedValue({ data_generation: 1 } as never)
    vi.spyOn(api, "threads").mockResolvedValue([])
    vi.spyOn(api, "projects").mockResolvedValue([])
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it("downloads this project and asks before adding it back", async () => {
    const user = userEvent.setup()
    render(<BackupTab query="" />)
    await user.click(screen.getByRole("button", { name: "Back up this project" }))
    expect(api.backupDownload).toHaveBeenCalledWith({
      kind: "project",
      project_id: "pj_one",
      omit_secrets: false,
    })
    const input = screen.getByLabelText("Choose a backup file")
    const file = new File(["PK"], "project.zwai-backup", { type: "application/octet-stream" })
    await user.upload(input, file)
    expect(await screen.findByText("Symbolic links were left out: 2.")).toBeInTheDocument()
    await user.click(await screen.findByRole("button", { name: "Add project from backup" }))
    expect(api.backupRestoreFile).toHaveBeenCalledWith(file, { replace: false, replace_project: false })
  })

  it("lets a running conversation be left out of the backup", async () => {
    const user = userEvent.setup()
    vi.mocked(api.backupDownload).mockReset()
    vi.mocked(api.backupDownload).mockRejectedValueOnce(new ApiError("engine: a backup or restore is already running, or a task is still active", 409, "backup_busy", {
      skippable: true,
      active: [{ id: "th_busy", title: "Busy task" }],
    }))
    render(<BackupTab query="" />)
    await user.click(screen.getByRole("button", { name: "Back up all data" }))
    expect(await screen.findByRole("dialog", { name: "Tasks are still running" })).toBeInTheDocument()
    expect(screen.getByText("Busy task")).toBeInTheDocument()
    expect(screen.getByText(/will not bring them back/)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Don't back up" }))
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Tasks are still running" })).not.toBeInTheDocument())
    expect(api.backupDownload).toHaveBeenCalledTimes(1)
  })

  it("retries the same backup without the running conversations", async () => {
    const user = userEvent.setup()
    vi.mocked(api.backupDownload).mockReset()
    vi.mocked(api.backupDownload)
      .mockRejectedValueOnce(new ApiError("busy", 409, "backup_busy", {
        skippable: true,
        active: [{ id: "th_busy", title: "Busy task" }],
      }))
      .mockResolvedValue({ blob: new Blob(["PK"]), skippedSymlinks: 0 })
    render(<BackupTab query="" />)
    await user.click(screen.getByRole("button", { name: "Back up all data" }))
    await user.click(await screen.findByRole("button", { name: "Skip these tasks and back up" }))
    await waitFor(() => expect(api.backupDownload).toHaveBeenCalledTimes(2))
    expect(api.backupDownload).toHaveBeenNthCalledWith(1, { kind: "full", omit_secrets: false })
    expect(api.backupDownload).toHaveBeenNthCalledWith(2, {
      kind: "full",
      omit_secrets: false,
      skip_active: true,
    })
  })

  it("shows progress while the backup is still being written", async () => {
    const user = userEvent.setup()
    let resolveDownload: (value: { blob: Blob; skippedSymlinks: number }) => void = () => {}
    let resolveProgress: (progress: { running: boolean; phase: "archive"; done: number; total: number }) => void = () => {}
    vi.mocked(api.backupDownload).mockReset()
    vi.mocked(api.backupDownload).mockReturnValue(new Promise((resolve) => {
      resolveDownload = resolve
    }))
    vi.mocked(api.backupProgress).mockReset()
    vi.mocked(api.backupProgress).mockReturnValue(new Promise((resolve) => {
      resolveProgress = resolve
    }))
    render(<BackupTab query="" />)
    void user.click(screen.getByRole("button", { name: "Back up all data" }))
    expect(await screen.findByRole("progressbar", { name: "Backup progress" })).toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveTextContent("Copying the database…")
    resolveProgress({ running: true, phase: "archive", done: 1, total: 2 })
    expect(await screen.findByRole("status")).toHaveTextContent("Writing the backup · 50%")
    expect(screen.getByRole("progressbar", { name: "Backup progress" })).toHaveAttribute("aria-valuenow", "50")
    resolveDownload({ blob: new Blob(["PK"]), skippedSymlinks: 0 })
    await waitFor(() => expect(screen.queryByRole("progressbar", { name: "Backup progress" })).not.toBeInTheDocument())
  })

  it("says when symbolic links were left out of the backup", async () => {
    const user = userEvent.setup()
    vi.mocked(api.backupDownload).mockReset()
    vi.mocked(api.backupDownload).mockResolvedValue({ blob: new Blob(["PK"]), skippedSymlinks: 3 })
    render(<BackupTab query="" />)
    await user.click(screen.getByRole("button", { name: "Back up all data" }))
    expect(await screen.findByRole("status")).toHaveTextContent("Backup written. 3 symbolic links were left out.")
  })

  it("does not offer to skip a backup that is already running", async () => {
    const user = userEvent.setup()
    useToasts.getState().clear()
    vi.mocked(api.backupDownload).mockReset()
    vi.mocked(api.backupDownload).mockRejectedValueOnce(new ApiError("busy", 409, "backup_busy", {}))
    render(<BackupTab query="" />)
    await user.click(screen.getByRole("button", { name: "Back up all data" }))
    await waitFor(() => expect(useToasts.getState().toasts.some((toast) => toast.message === "A backup, restore, or restart is already in progress.")).toBe(true))
    expect(screen.queryByRole("dialog", { name: "Tasks are still running" })).not.toBeInTheDocument()
    expect(api.backupDownload).toHaveBeenCalledTimes(1)
  })
})
