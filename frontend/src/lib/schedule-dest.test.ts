import { describe, expect, it } from "vitest"

import type { Project, Thread } from "@/lib/types"
import {
  RUNS_IN_NEW,
  applyRunsIn,
  livePickerThreads,
  pickerThreadLabel,
  pinnedPickerThreads,
  runsInFromSchedule,
  runsInThreadId,
  runsInThreadValue,
  unpinnedPickerGroups,
} from "./schedule-dest"

function thread(partial: Partial<Thread> & Pick<Thread, "id">): Thread {
  return {
    title: "",
    project_id: "",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: "2026-09-01T00:00:00.000Z",
    last_active_at: "2026-09-01T00:00:00.000Z",
    running: false,
    ...partial,
  }
}

function project(partial: Partial<Project> & Pick<Project, "id" | "name">): Project {
  return {
    system_prompt: "",
    workdir: "",
    resolved_workdir: "",
    memory_enabled: true,
    memory_dir: "",
    created_at: "",
    updated_at: "",
    ...partial,
  }
}

describe("runs-in destination", () => {
  it("maps a new conversation to standalone, and a picked chat to a thread wake", () => {
    expect(runsInThreadId(RUNS_IN_NEW)).toBeUndefined()
    expect(runsInThreadId(runsInThreadValue("th_1"))).toBe("th_1")
    expect(applyRunsIn({ prompt: "check" }, RUNS_IN_NEW, "none")).toEqual({
      prompt: "check",
      kind: "standalone",
    })
    expect(applyRunsIn({ prompt: "check" }, RUNS_IN_NEW, "pj_1")).toEqual({
      prompt: "check",
      kind: "standalone",
      project_id: "pj_1",
    })
    expect(applyRunsIn({ prompt: "check" }, runsInThreadValue("th_1"), "pj_1")).toEqual({
      prompt: "check",
      kind: "thread",
      thread_id: "th_1",
    })
    expect(runsInFromSchedule({ kind: "standalone", thread_id: "" })).toBe(RUNS_IN_NEW)
    expect(runsInFromSchedule({ kind: "thread", thread_id: "th_1" })).toBe(
      runsInThreadValue("th_1"),
    )
    expect(runsInFromSchedule({ kind: "standalone", thread_id: "th_ignore" })).toBe(
      RUNS_IN_NEW,
    )
  })

  it("drops archived chats and groups pinned ahead of project folders", () => {
    const rows = [
      thread({
        id: "th_old",
        title: "gone",
        archived: true,
        pinned: true,
      }),
      thread({
        id: "th_pin",
        title: "keep",
        pinned: true,
        pinned_at: "2026-09-20T00:00:00.000Z",
      }),
      thread({
        id: "th_free",
        title: "loose",
        last_active_at: "2026-09-21T00:00:00.000Z",
      }),
      thread({
        id: "th_in",
        title: "inside",
        project_id: "pj_1",
        last_active_at: "2026-09-22T00:00:00.000Z",
      }),
      thread({
        id: "th_ghost",
        title: "orphan",
        project_id: "pj_missing",
      }),
    ]
    const projects = [project({ id: "pj_1", name: "tools" })]
    expect(livePickerThreads(rows).map((r) => r.id)).toEqual([
      "th_pin",
      "th_free",
      "th_in",
      "th_ghost",
    ])
    expect(pinnedPickerThreads(rows).map((r) => r.id)).toEqual(["th_pin"])
    const groups = unpinnedPickerGroups(rows, projects)
    expect(groups.map((g) => ({ key: g.key, ids: g.threads.map((t) => t.id) }))).toEqual([
      { key: "", ids: ["th_free", "th_ghost"] },
      { key: "pj_1", ids: ["th_in"] },
    ])
    expect(groups[1]?.label).toBe("tools")
    expect(pickerThreadLabel(thread({ id: "th_x", title: "  " }), "Untitled")).toBe(
      "Untitled",
    )
  })
})
