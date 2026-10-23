import { beforeEach, describe, expect, it, vi } from "vitest"

import { api } from "@/lib/api"
import { emptyTidyReport } from "@/lib/skill-tidy"
import type { ProjectMemory } from "@/lib/types"
import { useProjects } from "./projects"

vi.mock("@/lib/api", () => ({ ApiError: class extends Error {}, api: { tidySkills: vi.fn() } }))
const memory: ProjectMemory = { dir: "", enabled: true, memory: { text: "", entries: [], chars: 0, limit: 2200, rev: "" }, skills: [] }

beforeEach(() => {
  vi.clearAllMocks()
  useProjects.setState({ memoryProjectId: "project", memory, projects: [], tidySlots: {}, tidyLive: undefined })
})

describe("memory tidy scan evidence", () => {
  it("keeps a server zero scan after the live catalog gains a promoted skill", async () => {
    let release = () => {}
    const gate = new Promise<void>((resolve) => { release = resolve })
    vi.mocked(api.tidySkills).mockImplementationOnce(async (_id, onEvent) => {
      onEvent?.({ phase: "scan" })
      await gate
      return { memory, report: emptyTidyReport(0), changes: [], folded: false }
    })
    const pending = useProjects.getState().tidySkills()
    expect(useProjects.getState().tidyLive?.scanned).toBe(0)
    useProjects.setState({ memory: { ...memory, skills: [{ name: "new-procedure", description: "When needed", updated_at: "" }] } })
    expect(useProjects.getState().tidyLive?.scanned).toBe(0)
    release()
    await pending
  })
})
