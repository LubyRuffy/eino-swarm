import { describe, expect, it } from "vitest"

import { agentRosterText } from "./agent-label"

describe("agentRosterText", () => {
  it("shows the id resume_agent takes, not a hash of the sequence", () => {
    expect(agentRosterText("helper/a/a/a-1/a/a", "helper/a/a/a-1/a/a-15")).toBe(
      "helper/a/a/a-1/a/a-15",
    )
    expect(agentRosterText("worker", "worker-4")).toBe("worker-4")
    expect(agentRosterText("worker", "worker-4")).not.toMatch(/#/)
    expect(agentRosterText("worker", "workers-1")).toBe("worker workers-1")
  })

  it("keeps an id that is not the job plus a suffix beside the job", () => {
    expect(agentRosterText("worker", "reviewer-1")).toBe("worker reviewer-1")
  })
})
