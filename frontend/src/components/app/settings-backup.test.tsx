import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { api } from "@/lib/api"
import { useProjects } from "@/store/projects"

import { BackupTab } from "./settings-backup"

vi.mock("@/lib/backup-file", () => ({
  nativeBackupFileAvailable: () => false,
  chooseBackupFile: vi.fn(),
}))

describe("backup settings", () => {
  beforeEach(() => {
    useProjects.setState({ projects: [{ id: "pj_one", name: "One" } as never], selectedId: "pj_one" })
    vi.spyOn(api, "backupDownload").mockResolvedValue(new Blob(["PK"]))
    vi.spyOn(api, "backupPreviewFile").mockResolvedValue({
      manifest: { kind: "project", includes_secrets: false, counts: { projects: 1, threads: 2 } },
      conflicts: [],
      action: "add-project",
    })
    vi.spyOn(api, "backupRestoreFile").mockResolvedValue({ kind: "project" })
    vi.spyOn(api, "meta").mockResolvedValue({ data_generation: 1 } as never)
    vi.spyOn(api, "threads").mockResolvedValue([])
    vi.spyOn(api, "projects").mockResolvedValue([])
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
    await user.click(await screen.findByRole("button", { name: "Add project from backup" }))
    expect(api.backupRestoreFile).toHaveBeenCalledWith(file, { replace: false, replace_project: false })
  })
})
