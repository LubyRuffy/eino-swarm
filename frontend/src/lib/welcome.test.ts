import { describe, expect, it } from "vitest"

import { emptyTranscript, MANAGER_ID } from "./transcript"
import { isWelcomePane, visibleManagerBlockCount } from "./welcome"

describe("isWelcomePane", () => {
  const blank = {
    activeId: "th_1",
    loaded: true,
    visibleManagerBlocks: 0,
    running: false,
    workerCount: 0,
    historyHasMore: false,
  }

  it("is the idea cards when no conversation is open", () => {
    expect(isWelcomePane({ ...blank, activeId: undefined })).toBe(true)
  })

  it("is the idea cards on a loaded conversation with nothing in it", () => {
    expect(isWelcomePane(blank)).toBe(true)
  })

  it("keeps the skeleton up until the tail has been applied", () => {
    expect(isWelcomePane({ ...blank, loaded: false })).toBe(false)
  })

  it("is the transcript once the manager has a visible row", () => {
    expect(isWelcomePane({ ...blank, visibleManagerBlocks: 1 })).toBe(false)
  })

  it("does not paint the idea cards over a running turn with no manager rows yet", () => {
    // The live-edge page is often just worker tool rows; the user bubble
    // sits on an older page. Welcome cards would hide the history sentinel.
    expect(isWelcomePane({ ...blank, running: true })).toBe(false)
  })

  it("does not paint the idea cards when workers are already on the roster", () => {
    expect(isWelcomePane({ ...blank, workerCount: 2 })).toBe(false)
  })

  it("does not paint the idea cards when older history still exists above the tail", () => {
    expect(isWelcomePane({ ...blank, historyHasMore: true })).toBe(false)
  })
})

describe("visibleManagerBlockCount", () => {
  it("does not treat a spawn row as the conversation itself", () => {
    const state = emptyTranscript()
    state.agentOrder = [MANAGER_ID]
    state.agents[MANAGER_ID] = {
      id: MANAGER_ID,
      role: "manager",
      status: "running",
      activity: "",
      blocks: [
        {
          id: "s1",
          kind: "spawn",
          agentId: MANAGER_ID,
          text: "worker",
          spawn: { agentId: "worker-1", role: "worker" },
          turnId: "tn_1",
          seq: 2,
          at: new Date().toISOString(),
        },
      ],
    }
    expect(visibleManagerBlockCount(state)).toBe(0)
  })
})
