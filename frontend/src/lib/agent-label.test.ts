import { describe, expect, it } from "vitest"

import { agentRosterLabel } from "./agent-label"

describe("agentRosterLabel", () => {
  it("shows the id resume_agent takes, not a hash of the sequence", () => {
    expect(agentRosterLabel("helper/a/a/a-1/a/a", "helper/a/a/a-1/a/a-15")).toEqual({
      name: "helper/a/a/a-1/a/a-15",
      tag: "",
    })
    expect(agentRosterLabel("code-reviewer", "code-reviewer-3")).toEqual({
      name: "code-reviewer-3",
      tag: "",
    })
    expect(agentRosterLabel("worker", "worker-4").name).toBe("worker-4")
    expect(agentRosterLabel("worker", "worker-4").tag).not.toMatch(/^#/)
    expect(agentRosterLabel("worker", "workers-1")).toEqual({
      name: "worker",
      tag: "workers-1",
    })
  })

  it("keeps an id that is not the job plus a suffix beside the job", () => {
    expect(agentRosterLabel("worker", "reviewer-1")).toEqual({
      name: "worker",
      tag: "reviewer-1",
    })
  })
})
