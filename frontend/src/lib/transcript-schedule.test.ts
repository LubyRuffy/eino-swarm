import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  emptyTranscript,
  reduceEvents,
  type TranscriptState,
} from "./transcript"
import { applyScheduleEvent } from "./transcript-schedule"
import type { SwarmEvent } from "./types"

let seq = 0
function ev(partial: Partial<SwarmEvent> & { kind: string }): SwarmEvent {
  const stored = partial.kind !== "delta" && partial.kind !== "reasoning_delta"
  return {
    thread_id: "th_1",
    turn_id: partial.turn_id ?? "tn_1",
    seq: partial.seq ?? (stored ? ++seq : 0),
    kind: partial.kind,
    agent_id: partial.agent_id ?? MANAGER_ID,
    role: partial.role,
    text: partial.text,
    tool_call_id: partial.tool_call_id,
    err: partial.err,
    images: partial.images,
    created_at: partial.created_at ?? new Date(1700000000000 + seq * 1000).toISOString(),
  }
}

function fold(events: SwarmEvent[], from?: TranscriptState) {
  return reduceEvents(from ?? emptyTranscript(), events)
}

function manager(state: TranscriptState) {
  return state.agents[MANAGER_ID]
}

function chatKinds(state: TranscriptState, turnId: string) {
  return Object.values(state.agents).flatMap((agent) =>
    agent.blocks.filter((b) => b.turnId === turnId),
  )
}

describe("scheduled-task transcript notices", () => {
  it("arms a wait as a manager notice without dumping the payload or a user bubble", () => {
    const payload = JSON.stringify({
      id: "sch_ab12",
      kind: "thread",
      title: "wake",
      next_run_at: "2026-09-19T02:00:00.000Z",
    })
    const handled = emptyTranscript()
    expect(
      applyScheduleEvent(handled, ev({ kind: "schedule", text: payload })),
    ).toBe(true)
    const state = fold([ev({ kind: "schedule", text: payload })])
    const [row] = manager(state).blocks
    expect(row.kind).toBe("notice")
    expect(row.text).toBe("A wait is armed.")
    expect(row.detail).toBe("sch_ab12")
    expect(row.kind).not.toBe("user")
    expect(row.text).not.toMatch(/sch_ab12|next_run_at|"kind"|wake/)
    expect(JSON.stringify(state)).not.toMatch(/CI|deploy|GitHub/)
  })

  it("marks the conversation working when a wait fires, without a user bubble", () => {
    const started = "2026-01-01T00:00:00.000Z"
    const handled = emptyTranscript()
    expect(
      applyScheduleEvent(
        handled,
        ev({ kind: "schedule_fired", turn_id: "tn_2", created_at: started, text: "Scheduled check." }),
      ),
    ).toBe(true)
    const state = fold([
      ev({
        kind: "schedule_fired",
        turn_id: "tn_2",
        created_at: started,
        text: "Scheduled check.",
      }),
    ])
    expect(state.running).toBe(true)
    expect(state.turns.at(-1)).toMatchObject({
      id: "tn_2",
      status: "running",
      startedAt: started,
    })
    const [row] = manager(state).blocks
    expect(row.kind).toBe("notice")
    expect(row.text).toBe("Scheduled check.")
    expect(row.kind).not.toBe("user")
    expect(row.text).not.toMatch(/This turn is a scheduled check/)
  })

  it("falls back to the fired chip when the event has no text", () => {
    const state = fold([ev({ kind: "schedule_fired", text: "" })])
    expect(manager(state).blocks[0].text).toBe("Scheduled check.")
  })

  it("keeps schedule_skipped off the chat list", () => {
    const handled = emptyTranscript()
    expect(applyScheduleEvent(handled, ev({ kind: "schedule_skipped", text: "busy" }))).toBe(
      true,
    )
    const state = fold([ev({ kind: "schedule_skipped", text: "busy" })])
    expect(state.agents[MANAGER_ID]?.blocks ?? []).toEqual([])
    expect(state.running).toBe(false)
  })

  it("notices a cancelled wait without showing the raw id", () => {
    const handled = emptyTranscript()
    expect(
      applyScheduleEvent(handled, ev({ kind: "schedule_cancelled", text: "sch_ab12" })),
    ).toBe(true)
    const state = fold([ev({ kind: "schedule_cancelled", text: "sch_ab12" })])
    const [row] = manager(state).blocks
    expect(row.kind).toBe("notice")
    expect(row.kind).not.toBe("user")
    expect(row.text).toBe("A wait was cancelled.")
    expect(row.text).not.toBe("sch_ab12")
    expect(row.detail).toBe("sch_ab12")
  })

  it("hides a quiet report's chat bubbles and remembers the turn", () => {
    const fired = fold([
      ev({ kind: "schedule_fired", turn_id: "tn_q", text: "Scheduled check." }),
      ev({ kind: "reasoning", turn_id: "tn_q", text: "checking" }),
      ev({ kind: "tool_call", turn_id: "tn_q", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "tool_result", turn_id: "tn_q", text: "{}", tool_call_id: "c1" }),
      ev({ kind: "agent_message", turn_id: "tn_q", text: "nothing to report" }),
    ])
    expect(chatKinds(fired, "tn_q").some((b) => b.kind === "notice")).toBe(true)
    expect(chatKinds(fired, "tn_q").some((b) => b.kind === "answer")).toBe(true)

    const previousQuiet = fired.quietTurns
    const quiet = fold(
      [
        ev({
          kind: "schedule_report",
          turn_id: "tn_q",
          text: JSON.stringify({ findings: "", quiet: true }),
        }),
      ],
      fired,
    )
    expect(applyScheduleEvent(emptyTranscript(), ev({ kind: "schedule_report", text: "{}" }))).toBe(
      true,
    )
    expect(quiet.quietTurns).toContain("tn_q")
    expect(quiet.turns.find((t) => t.id === "tn_q")?.quiet).toBe(true)
    expect(previousQuiet === quiet.quietTurns).toBe(false)
    const leftover = chatKinds(quiet, "tn_q")
    expect(leftover.filter((b) => ["user", "answer", "tool", "reasoning", "notice"].includes(b.kind))).toEqual(
      [],
    )

    const remembered = quiet.quietTurns
    const later = fold(
      [
        ev({ kind: "delta", turn_id: "tn_q", text: "still writing" }),
        ev({ kind: "agent_message", turn_id: "tn_q", text: "still writing" }),
        ev({ kind: "user_message", turn_id: "tn_q", text: "This turn is a scheduled check.\n\nContinue the wait." }),
        ev({ kind: "reasoning", turn_id: "tn_q", text: "more thought" }),
        ev({ kind: "tool_call", turn_id: "tn_q", text: "exec({})", tool_call_id: "c2" }),
      ],
      quiet,
    )
    const grown = chatKinds(later, "tn_q").filter((b) =>
      ["user", "answer", "tool", "reasoning", "notice"].includes(b.kind),
    )
    expect(grown).toEqual([])
    expect(later.quietTurns).not.toBe(remembered)
    expect(JSON.stringify(later)).not.toMatch(/CI|deploy|GitHub/)
  })

  it("keeps the fired chip and the reply when findings are non-empty", () => {
    const state = fold([
      ev({ kind: "schedule_fired", turn_id: "tn_f", text: "Scheduled check." }),
      ev({ kind: "agent_message", turn_id: "tn_f", text: "one thing changed" }),
      ev({
        kind: "schedule_report",
        turn_id: "tn_f",
        text: JSON.stringify({ findings: "one thing changed", quiet: false }),
      }),
    ])
    const blocks = chatKinds(state, "tn_f")
    expect(blocks.map((b) => b.kind)).toEqual(["notice", "answer"])
    expect(blocks[0].text).toBe("Scheduled check.")
    expect(blocks[1].text).toBe("one thing changed")
    expect(state.quietTurns ?? []).not.toContain("tn_f")
    expect(state.turns.find((t) => t.id === "tn_f")?.quiet).not.toBe(true)
    expect(blocks.some((b) => b.kind === "user")).toBe(false)
    expect(JSON.stringify(state)).not.toMatch(/This turn is a scheduled check/)
  })

  it("does not treat an empty-findings report without quiet as a findings turn", () => {
    const state = fold([
      ev({ kind: "schedule_fired", turn_id: "tn_e", text: "Scheduled check." }),
      ev({ kind: "agent_message", turn_id: "tn_e", text: "noise" }),
      ev({
        kind: "schedule_report",
        turn_id: "tn_e",
        text: JSON.stringify({ findings: "   ", quiet: false }),
      }),
    ])
    expect(state.quietTurns).toContain("tn_e")
    expect(chatKinds(state, "tn_e").filter((b) => b.kind === "answer")).toEqual([])
  })
})
