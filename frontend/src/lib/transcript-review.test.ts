import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  PENDING_EDIT_ID,
  emptyTranscript,
  placePendingEdit,
  reduceEvents,
  reviewPanelHint,
  rewindTranscript,
  type TranscriptState,
} from "./transcript"
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

describe("the memory review", () => {
  const review = (body: unknown, err?: string) =>
    ev({
      kind: "memory_review",
      agent_id: "memory-reviewer",
      role: "memory-reviewer",
      text: JSON.stringify(body),
      err,
    })

  // The reviewer is not a member of the swarm. Letting it into the roster
  // would show a worker that has nothing to display and never finishes.
  it("does not join the roster", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "done", text: "answer" }),
      review({ changed: true, notes: { add: 1 } }),
    ])
    expect(state.agentOrder).toEqual([MANAGER_ID])
    expect(state.agents["memory-reviewer"]).toBeUndefined()
  })

  it("says what it kept, counting notes and naming skills", () => {
    const state = fold([
      review({
        changed: true,
        notes: { add: 2, replace: 1 },
        skills: [{ name: "a-procedure", action: "create" }],
      }),
    ])
    const notices = manager(state).blocks.filter((b) => b.kind === "notice")
    expect(notices).toHaveLength(1)
    expect(notices[0].text).toBe(
      'Memory updated: 2 notes stored, 1 note revised, skill "a-procedure" recorded.',
    )
  })

  it("names a merged skill the same way it names a recorded one", () => {
    const state = fold([
      review({
        changed: true,
        skills: [{ name: "a-procedure", action: "merge", text: "a-procedure-notes, a-procedure-send" }],
      }),
    ])
    const notices = manager(state).blocks.filter((b) => b.kind === "notice")
    expect(notices).toHaveLength(1)
    expect(notices[0].text).toBe('Memory updated: skill "a-procedure" merged.')
  })

  it("previews what was written when the review asked to be verbose", () => {
    const state = fold([
      review({
        changed: true,
        notify: "verbose",
        notes: { add: 1 },
        skills: [{ name: "a-procedure", action: "create" }],
        changes: [
          { target: "memory", action: "add", text: "User prefers terse replies" },
          { target: "skill_manage", action: "create", name: "a-procedure", text: "when it applies" },
        ],
      }),
    ])
    expect(manager(state).blocks[0].text).toContain("+ note: User prefers terse replies")
    expect(manager(state).blocks[0].text).toContain("+ skill a-procedure: when it applies")
    expect(manager(state).blocks[0].quiet).toBeFalsy()
  })

  it("keeps a silent review in the transcript so Trace can still see it", () => {
    const state = fold([
      review({ changed: true, notify: "off", notes: { add: 1 } }),
    ])
    const [row] = manager(state).blocks.filter((b) => b.kind === "notice")
    expect(row.quiet).toBe(true)
    expect(row.text).toMatch(/Memory updated/)
  })

  // Most turns teach a project nothing. A row after every answer saying so
  // would train the reader to ignore the ones that matter.
  it("shows nothing when it kept nothing", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "done", text: "answer" }),
      review({ changed: false, note: "nothing durable" }),
    ])
    expect(manager(state).blocks.filter((b) => b.kind === "notice")).toHaveLength(0)
  })

  it("still answers a click that kept nothing", () => {
    expect(reviewPanelHint({ changed: false })).toMatch(/nothing new to keep/)
    expect(reviewPanelHint({ changed: true })).toBe("Review finished.")
    expect(reviewPanelHint({ changed: false, err: "model refused" })).toBe(
      "Memory review failed: model refused",
    )
  })

  // A review that fell over is worth a line: memory the user believes is
  // being kept, and is not, is the failure they cannot see.
  it("reports a failed review", () => {
    const state = fold([review({ changed: false, err: "model refused" })])
    expect(manager(state).blocks[0].text).toBe("Memory review failed: model refused")
  })

  it("survives a malformed payload without a block", () => {
    const state = fold([ev({ kind: "memory_review", text: "{not json" })])
    expect(manager(state)).toBeUndefined()
  })

  it("still moves the resume point, so a reconnect does not replay it", () => {
    const state = fold([review({ changed: false })])
    expect(state.lastSeq).toBeGreaterThan(0)
  })
})

describe("a generated conversation title", () => {
  // The default branch would paste the name into the transcript as a notice
  // and invent a "title-namer" worker in the roster.
  it("leaves no trace in the transcript", () => {
    const before = fold([ev({ kind: "user_message", text: "hi" })])
    const after = fold(
      [
        ev({
          kind: "title",
          agent_id: "title-namer",
          text: "Weekly status",
        }),
      ],
      before,
    )
    expect(after.agents[MANAGER_ID].blocks.filter((b) => b.kind !== "title")).toHaveLength(1)
    expect(after.agentOrder).toEqual([MANAGER_ID])
    expect(after.agents["title-namer"]).toBeUndefined()
    const title = after.agents[MANAGER_ID].blocks.find((b) => b.kind === "title")
    expect(title?.text).toBe("Weekly status")
    expect(title?.agentId).toBe("title-namer")
    expect(title?.quiet).toBe(true)
    expect(after.lastSeq).toBeGreaterThan(before.lastSeq)
  })
})

describe("a rolling session briefing", () => {
  it("leaves no chat row and no session-memory worker", () => {
    const before = fold([ev({ kind: "user_message", text: "hi" })])
    const after = fold(
      [
        ev({
          kind: "session_memory",
          agent_id: "session-memory",
          text: '{"summary":"prior work","through_seq":12}',
        }),
      ],
      before,
    )
    expect(after.agents[MANAGER_ID].blocks.filter((b) => b.kind !== "title" && b.kind !== "session_memory")).toHaveLength(1)
    expect(after.agentOrder).toEqual([MANAGER_ID])
    expect(after.agents["session-memory"]).toBeUndefined()
    const row = after.agents[MANAGER_ID].blocks.find((b) => b.kind === "session_memory")
    expect(row?.quiet).toBe(true)
    expect(row?.agentId).toBe("session-memory")
  })
})

describe("pasted images", () => {
  it("keeps image refs on the user and steer blocks so thumbs can render", () => {
    const refs = [{ id: "img_ab", name: "clip.png", mime: "image/png" }]
    const state = fold([
      ev({ kind: "user_message", text: "look", images: refs }),
      ev({ kind: "steer", text: "and this", images: refs }),
    ])
    const user = manager(state).blocks.find((b) => b.kind === "user")
    const steer = manager(state).blocks.find((b) => b.kind === "steer")
    expect(user?.images).toEqual(refs)
    expect(steer?.images).toEqual(refs)
  })
})

describe("rewind", () => {
  it("drops the named message and everything after it", () => {
    const state = fold([
      ev({ kind: "user_message", seq: 1, turn_id: "tn_1", text: "first" }),
      ev({ kind: "agent_message", seq: 2, turn_id: "tn_1", text: "answer one" }),
      ev({ kind: "done", seq: 3, turn_id: "tn_1", text: "answer one" }),
      ev({ kind: "user_message", seq: 4, turn_id: "tn_2", text: "second" }),
      ev({ kind: "agent_message", seq: 5, turn_id: "tn_2", text: "answer two" }),
    ])
    const after = fold([ev({ kind: "rewound", seq: 0, text: "4" })], state)
    const texts = manager(after).blocks.map((b) => b.text)
    expect(texts).toContain("first")
    expect(texts).not.toContain("second")
    expect(texts).not.toContain("answer two")
    expect(after.turns.map((t) => t.id)).toEqual(["tn_1"])
    expect(after.running).toBe(false)
    expect(after.pulse).toBeUndefined()
    expect(after.lastSeq).toBe(state.lastSeq)
  })

  it("keeps a pending edit at the cut so the bubble does not vanish", () => {
    const state = fold([
      ev({ kind: "user_message", seq: 1, turn_id: "tn_1", text: "first" }),
      ev({ kind: "agent_message", seq: 2, turn_id: "tn_1", text: "answer one" }),
      ev({ kind: "done", seq: 3, turn_id: "tn_1", text: "answer one" }),
      ev({ kind: "user_message", seq: 4, turn_id: "tn_2", text: "second" }),
      ev({ kind: "agent_message", seq: 5, turn_id: "tn_2", text: "answer two" }),
    ])
    const pending = placePendingEdit(rewindTranscript(state, 4), "edited")
    expect(manager(pending).blocks.map((b) => b.text)).toEqual([
      "first",
      "answer one",
      "edited",
    ])
    expect(pending.running).toBe(true)
    const afterRewound = fold([ev({ kind: "rewound", seq: 0, text: "4" })], pending)
    expect(manager(afterRewound).blocks.map((b) => b.text)).toEqual([
      "first",
      "answer one",
      "edited",
    ])
    expect(afterRewound.running).toBe(true)
    const afterUser = fold(
      [ev({ kind: "user_message", seq: 10, turn_id: "tn_3", text: "edited" })],
      afterRewound,
    )
    expect(
      manager(afterUser)
        .blocks.filter((b) => b.kind === "user")
        .map((b) => b.text),
    ).toEqual(["first", "edited"])
    expect(manager(afterUser).blocks.some((b) => b.id === PENDING_EDIT_ID)).toBe(false)
    expect(manager(afterUser).blocks.map((b) => b.text)).not.toContain("second")
    expect(manager(afterUser).blocks.map((b) => b.text)).not.toContain("answer two")
  })

  it("drops live deltas whose seq is zero", () => {
    const state = fold([
      ev({ kind: "user_message", seq: 1, text: "first" }),
      ev({ kind: "delta", text: "streaming" }),
    ])
    const after = fold([ev({ kind: "rewound", seq: 0, text: "1" })], state)
    expect(manager(after).blocks).toEqual([])
  })

  it("ignores a cut that is not a sequence number", () => {
    const before = fold([ev({ kind: "user_message", seq: 1, text: "first" })])
    const after = fold([ev({ kind: "rewound", seq: 0, text: "nope" })], before)
    expect(after).toEqual(before)
  })
})

describe("ask_user cards", () => {
  it("turns a pending ask_user call into a question block, then settles it", () => {
    const pending = fold([
      ev({
        kind: "tool_call",
        text: `ask_user({"questions":[{"id":"approach","prompt":"Which approach?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]})`,
        tool_call_id: "tc_ask",
      }),
    ])
    const card = manager(pending).blocks.find((b) => b.kind === "question")
    expect(card?.question?.pending).toBe(true)
    expect(card?.question?.questions[0]?.id).toBe("approach")
    const settled = fold(
      [
        ev({
          kind: "tool_result",
          tool_call_id: "tc_ask",
          text: `{"answers":{"approach":{"answers":["A"]}}}`,
        }),
      ],
      pending,
    )
    expect(manager(settled).blocks[0]?.question?.pending).toBe(false)
    expect(manager(settled).blocks[0]?.question?.answers).toEqual({ approach: "A" })
  })
})

describe("plan notices", () => {
  it("folds plan events into short notices, not the markdown body", () => {
    const state = fold([
      ev({ kind: "plan", text: "planning" }),
      ev({ kind: "plan_updated", text: "# Plan\n\nDo the work.\n" }),
    ])
    expect(manager(state).blocks.map((b) => b.text)).toEqual([
      "Planning.",
      "Plan updated.",
    ])
  })
})
