import { beforeEach, describe, expect, it, vi } from "vitest"

import { projectOf, useProjects } from "@/store/projects"
import type { Project, ProjectMemory } from "@/lib/types"
import { emptyTidyReport } from "@/lib/skill-tidy"

// The interesting behaviour here is which project's memory ends up on screen,
// so the API is faked and the panel is left out of it.
const { fake, FakeApiError } = vi.hoisted(() => {
  class FakeApiError extends Error {
    constructor(
      message: string,
      readonly status: number,
      readonly code?: string,
      readonly details?: Record<string, unknown>,
    ) {
      super(message)
      this.name = "ApiError"
    }
  }
  return {
    FakeApiError,
    fake: {
      projects: [] as unknown[],
      memoryDelays: {} as Record<string, number>,
      deleted: [] as string[],
      saved: [] as string[],
      fail: false,
      conflict: false,
      skills: {} as Record<string, { name: string; description: string; updated_at: string }[]>,
      reordered: [] as string[],
      tidied: [] as string[],
      tidyDelays: {} as Record<string, number>,
      tidyFolded: false,
      foldedSkills: {} as Record<string, { name: string; description: string; updated_at: string }[]>,
    },
  }
})

vi.mock("@/lib/api", () => ({
  ApiError: FakeApiError,
  api: {
    projects: async () => {
      if (fake.fail) throw new Error("server is down")
      return fake.projects
    },
    createProject: async (patch: { name?: string }) => {
      if (fake.fail) throw new Error("that is not a directory")
      return project(patch.name ?? "")
    },
    patchProject: async (id: string, patch: { name?: string }) => {
      if (fake.fail) throw new Error("that is not a directory")
      return { ...project(patch.name ?? id), id }
    },
    deleteProject: async (id: string) => {
      if (fake.fail) throw new Error("server is down")
      fake.deleted.push(id)
    },
    reorderProjects: async (ids: string[]) => {
      fake.reordered = ids
      if (fake.fail) throw new Error("server is down")
      return ids.map((id) => {
        const found = (fake.projects as Project[]).find((p) => p.id === id)
        return found ?? project(id)
      })
    },
    memory: async (id: string) => {
      const delay = fake.memoryDelays[id] ?? 0
      if (delay > 0) await new Promise((r) => setTimeout(r, delay))
      if (fake.fail) throw new Error("cannot read memory")
      return memory(id)
    },
    saveMemory: async (id: string, text: string, rev?: string) => {
      fake.saved.push(`${id}:${text}:${rev ?? ""}`)
      if (fake.conflict) {
        throw new FakeApiError("these notes were changed after you loaded them", 409, "conflict", {
          memory: { text: "what landed", entries: ["what landed"], chars: 11, limit: 2200, rev: "rev_now" },
        })
      }
      return { text, entries: [text], chars: text.length, limit: 2200, rev: "rev_saved" }
    },
    deleteSkill: async (id: string, name: string) => {
      fake.deleted.push(`${id}/${name}`)
    },
    tidySkills: async (id: string) => {
      const delay = fake.tidyDelays[id] ?? 0
      if (delay > 0) await new Promise((r) => setTimeout(r, delay))
      fake.tidied.push(id)
      if (fake.fail) throw new Error("cannot tidy skills")
      const skills = fake.foldedSkills[id] ?? fake.skills[id] ?? []
      const before = (fake.skills[id] ?? []).length
      const report = fake.tidyFolded
        ? {
            scanned: before,
            before,
            after: skills.length,
            families: 1,
            unchanged: 0,
            created: ["weekly-rollup"],
            deleted: ["weekly-rollup-notes", "weekly-rollup-send"],
            merged: [
              {
                keep: "weekly-rollup",
                dropped: ["weekly-rollup-notes", "weekly-rollup-send"],
                created: true,
              },
            ],
          }
        : emptyTidyReport(before)
      return {
        memory: { ...memory(id), skills, needs_tidy: false },
        report,
        changes: fake.tidyFolded
          ? [{ target: "skill_manage", action: "merge", name: "weekly-rollup" }]
          : [],
        folded: fake.tidyFolded,
      }
    },
  },
}))

function project(name: string): Project {
  return {
    id: `pj_${name}`,
    name,
    system_prompt: "",
    workdir: "",
    resolved_workdir: `/data/projects/pj_${name}/workspace`,
    memory_enabled: true,
    memory_dir: `/data/projects/pj_${name}/memory`,
    created_at: "",
    updated_at: "",
  }
}

function memory(id: string): ProjectMemory {
  return {
    dir: `/data/projects/${id}/memory`,
    enabled: true,
    memory: { text: id, entries: [id], chars: id.length, limit: 2200, rev: `rev_${id}` },
    skills: fake.skills[id] ?? [],
  }
}

beforeEach(() => {
  fake.projects = []
  fake.memoryDelays = {}
  fake.deleted.length = 0
  fake.saved.length = 0
  fake.fail = false
  fake.conflict = false
  fake.skills = {}
  fake.reordered = []
  fake.tidied.length = 0
  fake.tidyDelays = {}
  fake.tidyFolded = false
  fake.foldedSkills = {}
  useProjects.setState({
    projects: [],
    selectedId: undefined,
    memory: undefined,
    memoryProjectId: undefined,
    memoryLoading: false,
    memoryUnread: false,
    reviewing: false,
    reviewHint: undefined,
    pendingReviewTurnId: undefined,
    tidying: false,
    tidyReport: undefined,
    tidyError: undefined,
    error: undefined,
  })
})

describe("the project list", () => {
  // A project deleted in another window would otherwise leave the sidebar
  // filtering on an id the server has never heard of — an empty list with no
  // way back.
  it("drops a selection the server no longer knows", async () => {
    fake.projects = [project("a")]
    useProjects.setState({ selectedId: "pj_gone" })
    await useProjects.getState().refresh()
    expect(useProjects.getState().selectedId).toBeUndefined()

    useProjects.setState({ selectedId: "pj_a" })
    await useProjects.getState().refresh()
    expect(useProjects.getState().selectedId).toBe("pj_a")
  })

  it("keeps a failed refresh out of the list and reports it", async () => {
    fake.projects = [project("a")]
    await useProjects.getState().refresh()
    fake.fail = true
    await useProjects.getState().refresh()
    expect(useProjects.getState().projects).toHaveLength(1)
    expect(useProjects.getState().error).toBe("server is down")
  })

  it("shows a new project without waiting for a reload", async () => {
    await useProjects.getState().create({ name: "fresh" })
    expect(useProjects.getState().projects.map((p) => p.name)).toEqual(["fresh"])
  })

  it("pins a dragged project order", async () => {
    fake.projects = [project("a"), project("b")]
    await useProjects.getState().refresh()
    await useProjects.getState().reorder(["pj_b", "pj_a"])
    expect(fake.reordered).toEqual(["pj_b", "pj_a"])
    expect(useProjects.getState().projects.map((p) => p.id)).toEqual(["pj_b", "pj_a"])
  })

  // The dialog needs the server's message to put it against the field that
  // caused it, which an error swallowed into the store could not do.
  it("lets a refused create or edit reach the caller", async () => {
    fake.fail = true
    await expect(useProjects.getState().create({ name: "x" })).rejects.toThrow(
      "that is not a directory",
    )
    await expect(
      useProjects.getState().update("pj_a", { workdir: "/nope" }),
    ).rejects.toThrow("that is not a directory")
    expect(useProjects.getState().projects).toHaveLength(0)
  })

  it("replaces an edited project and re-reads its memory", async () => {
    fake.projects = [project("a")]
    await useProjects.getState().refresh()
    await useProjects.getState().loadMemory("pj_a")
    const updated = await useProjects.getState().update("pj_a", { name: "renamed" })
    expect(updated.name).toBe("renamed")
    expect(useProjects.getState().projects[0].name).toBe("renamed")
    // Turning memory off, or moving the directory, changes what the panel
    // should show; keeping the old snapshot would show the wrong thing.
    expect(useProjects.getState().memoryProjectId).toBe("pj_a")
    expect(useProjects.getState().memory).toBeDefined()
  })

  it("forgets a deleted project, its selection and its memory", async () => {
    fake.projects = [project("a")]
    await useProjects.getState().refresh()
    useProjects.getState().select("pj_a")
    await useProjects.getState().loadMemory("pj_a")
    await useProjects.getState().remove("pj_a")

    const state = useProjects.getState()
    expect(fake.deleted).toEqual(["pj_a"])
    expect(state.projects).toHaveLength(0)
    expect(state.selectedId).toBeUndefined()
    expect(state.memory).toBeUndefined()
  })

  it("keeps a project that could not be deleted", async () => {
    fake.projects = [project("a")]
    await useProjects.getState().refresh()
    fake.fail = true
    await useProjects.getState().remove("pj_a")
    expect(useProjects.getState().projects).toHaveLength(1)
    expect(useProjects.getState().error).toBe("server is down")
  })
})

describe("the memory panel's data", () => {
  // Showing one project's notes under another's name is worse than showing
  // none: the user would correct, or trust, the wrong project's memory.
  it("ignores a slow response for a project that is no longer open", async () => {
    fake.memoryDelays = { pj_slow: 30 }
    const { loadMemory } = useProjects.getState()
    const slow = loadMemory("pj_slow")
    await loadMemory("pj_fast")
    await slow

    expect(useProjects.getState().memoryProjectId).toBe("pj_fast")
    expect(useProjects.getState().memory?.memory.text).toBe("pj_fast")
  })

  it("stops loading and reports a memory it cannot read", async () => {
    fake.fail = true
    await useProjects.getState().loadMemory("pj_a")
    expect(useProjects.getState().memoryLoading).toBe(false)
    expect(useProjects.getState().error).toBe("cannot read memory")
  })

  it("clears the panel when no project is open", async () => {
    await useProjects.getState().loadMemory("pj_a")
    await useProjects.getState().loadMemory(undefined)
    // loadMemory() with nothing open reloads whatever is open, so the panel
    // is cleared only once the project itself is gone.
    expect(useProjects.getState().memoryProjectId).toBe("pj_a")
  })

  it("saves an edit against the project the panel is showing", async () => {
    await useProjects.getState().loadMemory("pj_a")
    await useProjects.getState().saveMemory("a corrected note")
    expect(fake.saved).toEqual(["pj_a:a corrected note:rev_pj_a"])
    expect(useProjects.getState().memory?.memory.text).toBe("a corrected note")
  })

  it("does nothing when asked to save with no project open", async () => {
    await useProjects.getState().saveMemory("orphan")
    expect(fake.saved).toEqual([])
  })

  it("refuses a stale save and adopts what is stored now", async () => {
    await useProjects.getState().loadMemory("pj_a")
    fake.conflict = true
    await expect(useProjects.getState().saveMemory("mine")).rejects.toMatchObject({
      code: "conflict",
    })
    expect(useProjects.getState().memory?.memory.text).toBe("what landed")
    expect(useProjects.getState().memory?.memory.rev).toBe("rev_now")
  })

  it("marks a write as unread until the Memory tab is opened", () => {
    useProjects.getState().noteMemoryWrite()
    expect(useProjects.getState().memoryUnread).toBe(true)
    useProjects.getState().seeMemory()
    expect(useProjects.getState().memoryUnread).toBe(false)
  })

  it("copies skills onto the project so the sidebar can list them", async () => {
    fake.projects = [project("a")]
    fake.skills = {
      pj_a: [{ name: "a-procedure", description: "when it applies", updated_at: "" }],
    }
    await useProjects.getState().refresh()
    await useProjects.getState().loadMemory("pj_a")
    expect(useProjects.getState().projects[0].skills?.map((s) => s.name)).toEqual([
      "a-procedure",
    ])
  })

  it("re-reads memory after a skill is deleted", async () => {
    await useProjects.getState().loadMemory("pj_a")
    await useProjects.getState().removeSkill("a-procedure")
    expect(fake.deleted).toEqual(["pj_a/a-procedure"])
  })

  it("ignores a skill deletion with no project open", async () => {
    await useProjects.getState().removeSkill("a-procedure")
    expect(fake.deleted).toEqual([])
  })

  it("folds overlapping skills and copies the index onto the project", async () => {
    fake.projects = [project("a")]
    fake.skills = {
      pj_a: [
        { name: "weekly-rollup-notes", description: "when filing", updated_at: "" },
        { name: "weekly-rollup-send", description: "when sending", updated_at: "" },
      ],
    }
    fake.foldedSkills = {
      pj_a: [{ name: "weekly-rollup", description: "when filing the week", updated_at: "" }],
    }
    fake.tidyFolded = true
    await useProjects.getState().refresh()
    await useProjects.getState().loadMemory("pj_a")
    await useProjects.getState().tidySkills()
    expect(fake.tidied).toEqual(["pj_a"])
    expect(useProjects.getState().tidying).toBe(false)
    expect(useProjects.getState().tidyReport?.created).toEqual(["weekly-rollup"])
    expect(useProjects.getState().tidyReport?.deleted).toEqual([
      "weekly-rollup-notes",
      "weekly-rollup-send",
    ])
    expect(useProjects.getState().memory?.skills.map((s) => s.name)).toEqual([
      "weekly-rollup",
    ])
    expect(useProjects.getState().projects[0].skills?.map((s) => s.name)).toEqual([
      "weekly-rollup",
    ])
  })

  it("says so when the catalog was already tidy", async () => {
    await useProjects.getState().loadMemory("pj_a")
    await useProjects.getState().tidySkills()
    expect(useProjects.getState().tidyReport?.merged).toEqual([])
    expect(useProjects.getState().tidyError).toBeUndefined()
  })

  it("stops spinning and reports a tidy that failed", async () => {
    await useProjects.getState().loadMemory("pj_a")
    fake.fail = true
    await useProjects.getState().tidySkills()
    expect(useProjects.getState().tidying).toBe(false)
    expect(useProjects.getState().tidyError).toBe("cannot tidy skills")
  })

  it("does nothing when asked to tidy with no project open", async () => {
    await useProjects.getState().tidySkills()
    expect(fake.tidied).toEqual([])
  })

  it("ignores a slow tidy for a project that is no longer open", async () => {
    fake.tidyDelays = { pj_slow: 30 }
    fake.tidyFolded = true
    fake.foldedSkills = {
      pj_slow: [{ name: "kept", description: "when it applies", updated_at: "" }],
    }
    await useProjects.getState().loadMemory("pj_slow")
    const slow = useProjects.getState().tidySkills()
    await useProjects.getState().loadMemory("pj_fast")
    await slow
    expect(useProjects.getState().memoryProjectId).toBe("pj_fast")
    expect(useProjects.getState().tidying).toBe(false)
    expect(useProjects.getState().memory?.skills).toEqual([])
    expect(useProjects.getState().tidyReport).toBeUndefined()
  })

  it("dismisses the last tidy report", async () => {
    await useProjects.getState().loadMemory("pj_a")
    await useProjects.getState().tidySkills()
    expect(useProjects.getState().tidyReport).toBeDefined()
    useProjects.getState().clearTidy()
    expect(useProjects.getState().tidyReport).toBeUndefined()
  })
})

describe("a Review now click", () => {
  // Auto-review is silent when it kept nothing. A click is a request for an
  // answer, so the panel has to say so even then — otherwise the sparkles
  // look broken.
  it("shows a hint when the review kept nothing", () => {
    useProjects.getState().beginReview()
    expect(useProjects.getState().reviewing).toBe(true)
    useProjects.getState().finishReview("tn_1", { changed: false })
    expect(useProjects.getState().reviewing).toBe(false)
    expect(useProjects.getState().reviewHint).toMatch(/nothing new to keep/)
  })

  it("still answers if the event arrives before the 202 names the turn", () => {
    useProjects.getState().beginReview()
    useProjects.getState().finishReview("tn_early", { changed: true })
    expect(useProjects.getState().reviewing).toBe(false)
    expect(useProjects.getState().reviewHint).toBe("Review finished.")
    useProjects.getState().awaitReview("tn_early")
    expect(useProjects.getState().reviewing).toBe(false)
  })

  it("ignores a review for a different turn once it knows which one it asked for", () => {
    useProjects.getState().beginReview()
    useProjects.getState().awaitReview("tn_1")
    useProjects.getState().finishReview("tn_other", { changed: true })
    expect(useProjects.getState().reviewing).toBe(true)
    useProjects.getState().finishReview("tn_1", { changed: false })
    expect(useProjects.getState().reviewing).toBe(false)
  })

  it("does not steal an auto-review that nobody clicked", () => {
    useProjects.getState().finishReview("tn_1", { changed: true })
    expect(useProjects.getState().reviewHint).toBeUndefined()
  })
})

describe("projectOf", () => {
  it("finds the project a conversation belongs to, and nothing otherwise", () => {
    const projects = [project("a"), project("b")]
    expect(projectOf(projects, "pj_b")?.name).toBe("b")
    expect(projectOf(projects, undefined)).toBeUndefined()
    expect(projectOf(projects, "pj_gone")).toBeUndefined()
  })
})
