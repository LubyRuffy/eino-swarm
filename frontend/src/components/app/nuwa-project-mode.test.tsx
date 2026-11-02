import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { RightPanel } from "./panel"
import { emptyTranscript } from "@/lib/transcript"
import type { Project } from "@/lib/types"

vi.mock("./nuwa-panel", () => ({ NuwaPanel: () => <p>Recorded project work</p> }))
const project: Project = {
  id: "p", name: "P", system_prompt: "", workdir: "", resolved_workdir: "",
  memory_enabled: false, memory_dir: "", created_at: "", updated_at: "",
}
const props = {
  tab: "nuwa" as const, transcript: emptyTranscript(), files: [], workspace: "", turns: [],
  onTabChange: vi.fn(), onSelectAgent: vi.fn(), onUpload: async () => {},
  onDeleteFile: vi.fn(), onRefreshFiles: vi.fn(), onReveal: vi.fn(),
}

describe("Project collaboration mode", () => {
  it("keeps ordinary projects on the existing Agents view", () => {
    render(<RightPanel {...props} nuwaProject={project} />)
    expect(screen.queryByRole("tab", { name: "Collaboration" })).toBeNull()
    expect(screen.getByRole("tab", { name: "Agents" })).toHaveAttribute("data-state", "active")
  })

  it("keeps recorded collaboration reachable after the mode is turned off", () => {
    render(<RightPanel {...props} nuwaProject={{ ...project, nuwa_enabled: false, nuwa_revision: 2 }} />)
    expect(screen.getByRole("tab", { name: "Collaboration" })).toHaveAttribute("data-state", "active")
    expect(screen.getByText("Recorded project work")).toBeInTheDocument()
  })
})
