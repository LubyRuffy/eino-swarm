import { describe, expect, it } from "vitest"

import { agentRosterLabel } from "./agent-label"

describe("agentRosterLabel", () => {
  it("shows a job word and the sequence when the role is a path", () => {
    expect(agentRosterLabel("helper/a/a/a-1/a/a", "helper/a/a/a-1/a/a-15")).toEqual({
      name: "helper",
      tag: "#15",
    })
  })

  it("drops a copied sequence on the first path segment", () => {
    expect(agentRosterLabel("helper-01/a", "helper-01/a-4")).toEqual({
      name: "helper",
      tag: "#4",
    })
  })

  it("keeps a real hyphenated job name", () => {
    expect(agentRosterLabel("code-reviewer", "code-reviewer-3")).toEqual({
      name: "code-reviewer",
      tag: "#3",
    })
  })

  it("keeps an id that is not the role plus a number", () => {
    expect(agentRosterLabel("worker", "reviewer-1")).toEqual({
      name: "worker",
      tag: "reviewer-1",
    })
  })
})
