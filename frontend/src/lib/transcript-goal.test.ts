import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  emptyTranscript,
  reduceEvents,
  splitQueuedSteers,
  type TranscriptState,
} from "./transcript"
import { GOAL_SESSION_WRAP_STEER, compactIsStarting } from "./transcript-notices"
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

describe("standing objective and compact notices", () => {
  it("sets a notice on the manager without minting a worker", () => {
    const state = fold([
      ev({ kind: "goal", text: "keep going", agent_id: "manager" }),
    ])
    expect(manager(state).blocks[0].text).toBe("Standing objective set.")
    expect(state.agentOrder).toEqual([MANAGER_ID])
  })

  it("says when the objective is cleared", () => {
    const state = fold([ev({ kind: "goal", text: "" })])
    expect(manager(state).blocks[0].text).toBe("Standing objective cleared.")
  })

  it("marks the conversation working when auto-continue starts the next turn", () => {
    const started = "2026-01-01T00:00:00.000Z"
    const state = fold([
      ev({ kind: "user_message", text: "go", turn_id: "tn_1" }),
      ev({ kind: "done", turn_id: "tn_1" }),
      ev({
        kind: "goal_continued",
        turn_id: "tn_2",
        created_at: started,
        text: "Continuing the standing objective.",
      }),
    ])
    expect(state.running).toBe(true)
    expect(state.turns.at(-1)).toMatchObject({
      id: "tn_2",
      status: "running",
      startedAt: started,
    })
    expect(manager(state).status).toBe("running")
  })

  // Manager `done` parks leftover workers. Painting them finished is how
  // wait_agents sat next to a Done roster while the worker was still in exec.
  it("keeps a parked worker running across auto-continue", () => {
    const state = fold([
      ev({ kind: "user_message", text: "go", turn_id: "tn_1" }),
      ev({
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        text: "do the assigned work",
        turn_id: "tn_1",
      }),
      ev({
        kind: "tool_call",
        agent_id: "worker-1",
        text: "exec({})",
        tool_call_id: "c1",
        turn_id: "tn_1",
      }),
      ev({ kind: "done", turn_id: "tn_1", text: "progress" }),
      ev({
        kind: "goal_continued",
        turn_id: "tn_2",
        text: "Continuing the standing objective.",
      }),
      ev({
        kind: "tool_call",
        agent_id: "manager",
        text: `wait_agents(${JSON.stringify({ agent_ids: ["worker-1"] })})`,
        tool_call_id: "w1",
        turn_id: "tn_2",
      }),
    ])
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.agents["worker-1"].activity).toBe("exec")
    const [tool] = state.agents["worker-1"].blocks.filter((b) => b.kind === "tool")
    expect(tool.tool?.pending).toBe(true)
    expect(JSON.stringify(state)).not.toMatch(/notes\.md|summarize|look into this/)
  })

  it("revives a worker that is still emitting after a stale done tick", () => {
    const parked = fold([
      ev({ kind: "user_message", text: "go" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker" }),
      ev({ kind: "cleanup", text: "stopped 1 sub-agent(s) still running" }),
    ])
    expect(parked.agents["worker-1"].status).toBe("done")
    const live = fold(
      [
        ev({
          kind: "tool_call",
          agent_id: "worker-1",
          text: "exec({})",
          tool_call_id: "c9",
        }),
      ],
      parked,
    )
    expect(live.agents["worker-1"].status).toBe("running")
    expect(live.agents["worker-1"].activity).toBe("exec")
  })

  it("closes leftover workers when cleanup actually killed them", () => {
    const state = fold([
      ev({ kind: "user_message", text: "go" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker" }),
      ev({
        kind: "tool_call",
        agent_id: "worker-1",
        text: "exec({})",
        tool_call_id: "c1",
      }),
      ev({ kind: "cleanup", text: "stopped 1 sub-agent(s) still running at the end of the turn" }),
      ev({ kind: "done", text: "progress" }),
    ])
    expect(state.agents["worker-1"].status).toBe("done")
    const [tool] = state.agents["worker-1"].blocks.filter((b) => b.kind === "tool")
    expect(tool.tool?.pending).toBe(false)
    expect(JSON.stringify(state)).not.toMatch(/notes\.md|summarize|look into this/)
  })

  it("does not paint a parked worker done when the next human turn starts", () => {
    const state = fold([
      ev({ kind: "user_message", text: "go", turn_id: "tn_1" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker", turn_id: "tn_1" }),
      ev({ kind: "done", text: "progress", turn_id: "tn_1" }),
      ev({ kind: "user_message", text: "keep going", turn_id: "tn_2" }),
    ])
    expect(state.agents["worker-1"].status).toBe("running")
  })

  it("marks the conversation working when a standing objective is resumed", () => {
    const started = "2026-01-01T02:00:00.000Z"
    const state = fold([
      ev({
        kind: "goal_resumed",
        turn_id: "tn_3",
        created_at: started,
        text: "Resuming the standing objective.",
      }),
    ])
    expect(state.running).toBe(true)
    expect(state.turns[0]).toMatchObject({
      id: "tn_3",
      status: "running",
      startedAt: started,
    })
  })

  it("notices complete, continue and cap without minting workers", () => {
    const complete = fold([ev({ kind: "goal_complete", agent_id: "manager" })])
    expect(manager(complete).blocks[0].text).toBe("Standing objective completed.")
    const continued = fold([ev({ kind: "goal_continued", text: "Continuing the standing objective." })])
    expect(manager(continued).blocks[0].text).toBe("Continuing the standing objective.")
    const capped = fold([ev({ kind: "goal_capped", text: '{"auto_turns":12,"cap":12}' })])
    expect(manager(capped).blocks[0].text).toMatch(/Stopped auto-continuing/)
    expect(manager(capped).blocks[0].text).toMatch(/Press Start on the goal/)
    expect(manager(capped).blocks[0].text).not.toMatch(/auto_turns/)
    const paused = fold([ev({ kind: "goal_capped", text: '{"reason":"interrupted"}' })])
    expect(manager(paused).blocks[0].text).toMatch(/^Standing objective paused\./)
    expect(manager(paused).blocks[0].text).toMatch(/Press Start on the goal/)
    expect(manager(paused).blocks[0].text).not.toMatch(/interrupted/)
    expect(capped.agentOrder).toEqual([MANAGER_ID])
    const blocked = fold([ev({ kind: "goal_blocked", text: '{"reason":"needs an external change"}' })])
    expect(manager(blocked).blocks[0].text).toMatch(/blocked/)
    const retried = fold([ev({ kind: "model_retry", text: '{"attempt":1,"cap":2}' })])
    expect(manager(retried).blocks[0].text).toBe("Retrying after a model error.")
    expect(manager(retried).blocks[0].text).not.toMatch(/attempt/)
    const edited = fold([ev({ kind: "goal_edited", text: "keep going" })])
    expect(manager(edited).blocks[0].text).toBe("Standing objective updated.")
    const resumed = fold([ev({ kind: "goal_resumed", text: "Resuming the standing objective." })])
    expect(manager(resumed).blocks[0].text).toBe("Resuming the standing objective.")
    const idle = fold([
      ev({
        kind: "goal_idle",
        text: "Stopped auto-continuing: the last continuation made no progress.",
      }),
    ])
    expect(manager(idle).blocks[0].text).toMatch(/no progress/)
    expect(manager(idle).blocks[0].text).toMatch(/Press Start on the goal/)
  })

  it("notices a forced session without pasting the JSON", () => {
    const byTime = fold([ev({ kind: "goal_session", text: '{"reason":"time","elapsed_ms":9000,"rounds":4}' })])
    expect(manager(byTime).blocks[0].text).toBe("Work session ended after the time cap.")
    expect(byTime.turns[0]?.session).toBe(true)
    const byIters = fold([ev({ kind: "goal_session", text: '{"reason":"iterations","elapsed_ms":12,"rounds":1}' })])
    expect(manager(byIters).blocks[0].text).toBe("Work session ended after the tool-round cap.")
    expect(byIters.turns[0]?.session).toBe(true)
  })

  it("marks an auto-continue turn as a session", () => {
    const continued = fold([ev({ kind: "goal_continued", text: "Continuing the standing objective." })])
    expect(continued.turns[0]?.session).toBe(true)
  })

  it("does not paste the briefing JSON into the transcript", () => {
    const state = fold([
      ev({
        kind: "compacted",
        agent_id: "compact-summarizer",
        text: '{"summary":"Prior work: folded","through_seq":9}',
      }),
    ])
    expect(manager(state).blocks[0].text).toBe(
      "Earlier turns were folded into a briefing. The transcript is unchanged.",
    )
    expect(manager(state).blocks[0].text).not.toMatch(/through_seq/)
    expect(manager(state).blocks[0].text).not.toMatch(/Prior work/)
    expect(manager(state).blocks[0].detail).toBe("Prior work: folded")
    expect(state.agents["compact-summarizer"]).toBeUndefined()
  })

  it("names auto-compact with the token counts instead of the payload", () => {
    const starting = fold([
      ev({
        kind: "compacted",
        seq: 0,
        agent_id: "compact-summarizer",
        text: '{"auto":true,"phase":"start","tokens_before":91200}',
      }),
    ])
    expect(manager(starting).blocks[0].text).toBe("Compressing conversation context…")
    expect(manager(starting).blocks[0].detail).toBeUndefined()
    expect(
      compactIsStarting({
        text: '{"auto":true,"phase":"start","tokens_before":91200}',
      }),
    ).toBe(true)
    expect(
      compactIsStarting({
        text: '{"auto":true,"tokens_before":91200,"tokens_after":1400}',
      }),
    ).toBe(false)
    expect(compactIsStarting({ err: "the model returned nothing usable as a briefing" })).toBe(
      false,
    )
    const done = fold([
      ev({
        kind: "compacted",
        agent_id: "compact-summarizer",
        text: '{"auto":true,"summary":"folded work","through_seq":9,"tokens_before":91200,"tokens_after":1400}',
      }),
    ])
    expect(manager(done).blocks[0].text).toBe(
      "Context compressed (91200 → 1400 tokens). The transcript is unchanged.",
    )
    expect(manager(done).blocks[0].text).not.toMatch(/folded work/)
    expect(manager(done).blocks[0].text).not.toMatch(/through_seq/)
    expect(manager(done).blocks[0].detail).toBe("folded work")
  })

  it("surfaces a failed compact as the recorded error", () => {
    const state = fold([
      ev({ kind: "compacted", err: "the model returned nothing usable as a briefing" }),
    ])
    expect(manager(state).blocks[0].text).toBe(
      "the model returned nothing usable as a briefing",
    )
    expect(manager(state).blocks[0].detail).toBeUndefined()
  })

  it("hides a session wrap-up steer so it does not look like human guidance", () => {
    expect(GOAL_SESSION_WRAP_STEER).toContain("block_goal")
    const legacy =
      "This work session is ending. Summarize current progress. Do not start new long-running work. Call complete_goal only if the standing objective is actually satisfied. Otherwise stop this turn without asking the human."
    const state = fold([
      ev({ kind: "user_message", text: "go" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "steer", text: GOAL_SESSION_WRAP_STEER }),
      ev({ kind: "steer", text: legacy }),
      ev({ kind: "steer", text: "focus on the second part" }),
    ])
    const blocks = manager(state).blocks
    expect(blocks.some((b) => b.kind === "steer" && b.text === GOAL_SESSION_WRAP_STEER)).toBe(
      false,
    )
    expect(blocks.some((b) => b.kind === "steer" && b.text === legacy)).toBe(false)
    expect(blocks.filter((b) => b.kind === "steer").map((b) => b.text)).toEqual([
      "focus on the second part",
    ])
    const { queued } = splitQueuedSteers(blocks, true)
    expect(queued.map((b) => b.text)).toEqual(["focus on the second part"])
  })
})
