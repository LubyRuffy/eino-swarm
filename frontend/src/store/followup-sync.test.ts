import { describe, expect, it } from "vitest"

import {
  bumpFollowups,
  dropMatchingFollowups,
  isFollowupGeneration,
  liveTurnUserText,
} from "./followup-sync"

describe("dropMatchingFollowups", () => {
  it("removes waiting rows whose text is the live turn", () => {
    const rows = [
      { id: "fu_1", thread_id: "th", seq: 1, text: "same words", created_at: "" },
      { id: "fu_2", thread_id: "th", seq: 2, text: "later", created_at: "" },
    ]
    expect(dropMatchingFollowups(rows, "same words").map((f) => f.id)).toEqual([
      "fu_2",
    ])
  })
})

describe("followup generation", () => {
  it("stale fetches lose to a later mutation", () => {
    const stale = bumpFollowups()
    bumpFollowups()
    expect(isFollowupGeneration(stale)).toBe(false)
  })
})

describe("liveTurnUserText", () => {
  it("reads the running turn's user bubble", () => {
    expect(
      liveTurnUserText(
        { turn_id: "tn_1" },
        {
          turns: [{ id: "tn_1", userText: "same words", status: "running", agentIds: [] }],
          agents: {},
        },
      ),
    ).toBe("same words")
  })
})
