import { describe, expect, it } from "vitest"

import { agentRosterText } from "./agent-label"

describe("agentRosterText", () => {
  it("shows a job word instead of a path", () => {
    expect(agentRosterText("helper/a/a/a-1/a/a", "helper/a/a/a-1/a/a-15")).toBe(
      "helper #15",
    )
  })

  it("keeps an id that is not the role plus a number", () => {
    expect(agentRosterText("worker", "reviewer-1")).toBe("worker reviewer-1")
  })
})
