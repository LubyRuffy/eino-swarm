import { describe, expect, it } from "vitest"

import {
  applyComposerPredictionEvent,
  nextComposerPrediction,
  predictionToInsert,
  predictionsEnabled,
  type ComposerPrediction,
} from "./composer-prediction"
import { MANAGER_ID, emptyTranscript, placePendingEdit, reduceEvent, rewindTranscript } from "./transcript"
import type { SwarmEvent } from "./types"

function ev(partial: Partial<SwarmEvent> & { kind: string; seq: number }): SwarmEvent {
  return {
    thread_id: "th_1",
    turn_id: "tn_1",
    agent_id: MANAGER_ID,
    created_at: "2026-11-03T00:00:00Z",
    ...partial,
  }
}

describe("next-step suggestions", () => {
  it("keeps a suggestion for the latest idle turn and drops the rest", () => {
    expect(predictionsEnabled(undefined)).toBe(true)
    expect(predictionsEnabled(false)).toBe(false)
    expect(predictionToInsert("", "  next  ")).toBe("next")
    expect(predictionToInsert("typed", "next")).toBeNull()
    expect(predictionToInsert("", "  ")).toBeNull()

    const current: ComposerPrediction = { text: "older", turnId: "tn_0" }
    expect(nextComposerPrediction([{ id: "tn_1" }], current, { turn_id: "tn_0", text: "stale" })).toEqual(current)
    expect(nextComposerPrediction([{ id: "tn_1" }], current, { turn_id: "tn_1", text: "  next  " })).toEqual({
      text: "next",
      turnId: "tn_1",
    })
    expect(nextComposerPrediction([{ id: "tn_1" }], current, { turn_id: "tn_1", err: "no suggestion" })).toBeUndefined()
  })

  it("shows the suggestion from history without a chat row", () => {
    let state = emptyTranscript()
    state = reduceEvent(state, ev({ kind: "user_message", seq: 1, text: "hello" }))
    state = reduceEvent(state, ev({ kind: "done", seq: 2, text: "finished" }))
    state = reduceEvent(state, ev({
      kind: "composer_prediction",
      seq: 3,
      agent_id: "composer-prediction",
      text: "Continue from the latest reply",
    }))
    expect(state.prediction).toEqual({ text: "Continue from the latest reply", turnId: "tn_1" })
    expect(state.agents["composer-prediction"]).toBeUndefined()
    const blocks = state.agents[MANAGER_ID]?.blocks ?? []
    expect(blocks.some((block) => block.kind !== "trace" && block.text?.includes("Continue from the latest reply"))).toBe(false)
    expect(blocks.some((block) => block.kind === "trace" && block.quiet && block.traceKind === "composer_prediction")).toBe(true)

    state = reduceEvent(state, ev({ kind: "user_message", seq: 4, turn_id: "tn_2", text: "again" }))
    expect(state.prediction).toBeUndefined()
  })

  it("clears a suggestion when the conversation moves or the turn declines", () => {
    const seeded = {
      ...emptyTranscript(),
      prediction: { text: "next", turnId: "tn_1" },
    }
    expect(reduceEvent(seeded, ev({ kind: "schedule_fired", seq: 2, text: "check" })).prediction).toBeUndefined()
    expect(reduceEvent(seeded, ev({ kind: "goal_continued", seq: 2 })).prediction).toBeUndefined()
    expect(reduceEvent(seeded, ev({ kind: "resumed", seq: 2 })).prediction).toBeUndefined()
    expect(rewindTranscript(seeded, 1).prediction).toBeUndefined()
    expect(placePendingEdit(seeded, "edited").prediction).toBeUndefined()

    let state = emptyTranscript()
    state = reduceEvent(state, ev({ kind: "user_message", seq: 1, text: "hello" }))
    state = reduceEvent(state, ev({ kind: "done", seq: 2, text: "finished" }))
    state = reduceEvent(state, ev({ kind: "composer_prediction", seq: 3, text: "next" }))
    state = reduceEvent(state, ev({ kind: "composer_prediction", seq: 4, err: "no suggestion" }))
    expect(state.prediction).toBeUndefined()
  })

  it("leaves an unrelated event's suggestion in place", () => {
    const state = {
      ...emptyTranscript(),
      turns: [{ id: "tn_1", userText: "", status: "done" as const }],
      prediction: { text: "next", turnId: "tn_1" },
    }
    applyComposerPredictionEvent(state, { kind: "usage", turn_id: "tn_1" })
    expect(state.prediction?.text).toBe("next")
  })
})
