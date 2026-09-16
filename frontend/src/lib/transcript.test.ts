import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  PENDING_EDIT_ID,
  collapseLiveEvents,
  emptyTranscript,
  liveWorkers,
  placePendingEdit,
  reduceEvents,
  reviewPanelHint,
  rewindTranscript,
  splitQueuedSteers,
  splitToolCall,
  summarise,
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

describe("streamed text", () => {
  // The server sends the accumulated string, not the increment. Appending is
  // the bug that turns "Hello there" into "HelloHello there".
  it("replaces the open block instead of appending", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "delta", text: "Hello" }),
      ev({ kind: "delta", text: "Hello there" }),
      ev({ kind: "delta", text: "Hello there, world" }),
    ])
    const answers = manager(state).blocks.filter((b) => b.kind === "answer")
    expect(answers).toHaveLength(1)
    expect(answers[0].text).toBe("Hello there, world")
    expect(answers[0].streaming).toBe(true)
  })

  it("closes the thinking block when the answer starts", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "reasoning_delta", text: "Let me think" }),
      ev({ kind: "delta", text: "Here" }),
    ])
    const [reasoning] = manager(state).blocks.filter((b) => b.kind === "reasoning")
    expect(reasoning.streaming).toBe(false)
    expect(reasoning.text).toBe("Let me think")
  })

  // The complete text arrives after the deltas; it must land in the same block
  // rather than duplicating the answer under it.
  it("folds the complete message into the streamed block", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "delta", text: "Partial" }),
      ev({ kind: "agent_message", text: "Partial and complete" }),
    ])
    const answers = manager(state).blocks.filter((b) => b.kind === "answer")
    expect(answers).toHaveLength(1)
    expect(answers[0].text).toBe("Partial and complete")
    expect(answers[0].streaming).toBeFalsy()
  })

  // A replay has no deltas at all: only the stored complete events.
  it("renders a replay with no deltas", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "reasoning", text: "thought about it" }),
      ev({ kind: "agent_message", text: "the answer" }),
      ev({ kind: "done", text: "the answer" }),
    ])
    const kinds = manager(state).blocks.map((b) => b.kind)
    expect(kinds).toEqual(["user", "reasoning", "answer"])
    expect(state.running).toBe(false)
  })

  // Thinking, a tool call, then more thinking is two separate thoughts — not
  // one block that swallows the tool call's context.
  it("starts a new thinking block after a tool call", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "reasoning_delta", text: "first thought" }),
      ev({ kind: "tool_call", text: 'read({"path":"a"})', tool_call_id: "c1" }),
      ev({ kind: "tool_result", text: "contents", tool_call_id: "c1" }),
      ev({ kind: "reasoning_delta", text: "second thought" }),
    ])
    const thoughts = manager(state).blocks.filter((b) => b.kind === "reasoning")
    expect(thoughts.map((t) => t.text)).toEqual(["first thought", "second thought"])
  })
})

describe("tool calls", () => {
  // Parallel calls come back in whatever order they finish, so results are
  // paired by id. Pairing by position puts one worker's output under another's
  // call.
  it("pairs results with calls by id, out of order", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: 'read({"path":"a"})', tool_call_id: "call-a" }),
      ev({ kind: "tool_call", text: 'read({"path":"b"})', tool_call_id: "call-b" }),
      ev({ kind: "tool_result", text: "B contents", tool_call_id: "call-b" }),
      ev({ kind: "tool_result", text: "A contents", tool_call_id: "call-a" }),
    ])
    const tools = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tools).toHaveLength(2)
    expect(tools[0].tool?.args).toContain('"a"')
    expect(tools[0].tool?.result).toBe("A contents")
    expect(tools[1].tool?.result).toBe("B contents")
    expect(tools.every((t) => t.tool?.pending === false)).toBe(true)
  })

  it("shows a call with no result yet as pending", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "web_search({})", tool_call_id: "c1" }),
    ])
    const [tool] = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tool.tool?.pending).toBe(true)
    expect(tool.tool?.name).toBe("web_search")
  })

  // Interrupt kills in-flight tools without a tool_result. Leaving them
  // pending keeps the spinner next to a row that already says the turn stopped.
  it("closes tools left pending when the turn is interrupted", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c2" }),
      ev({ kind: "error", err: "interrupted" }),
    ])
    const tools = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tools).toHaveLength(2)
    expect(tools.every((t) => t.tool?.pending === false)).toBe(true)
    expect(tools.every((t) => t.tool?.failed === true)).toBe(true)
    expect(tools.every((t) => t.tool?.result === "interrupted")).toBe(true)
    expect(state.running).toBe(false)
  })

  it("closes tools left pending when the turn ends cleanly", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "done", text: "finished early" }),
    ])
    const [tool] = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tool.tool?.pending).toBe(false)
    expect(tool.tool?.failed).toBe(true)
  })

  it("does not mark a completed tool failed when the turn is interrupted", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "tool_result", text: "ok", tool_call_id: "c1" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c2" }),
      ev({ kind: "error", err: "interrupted" }),
    ])
    const tools = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tools[0].tool).toMatchObject({ pending: false, failed: false, result: "ok" })
    expect(tools[1].tool).toMatchObject({ pending: false, failed: true, result: "interrupted" })
  })

  it("closes a worker's pending tools when that agent finishes", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker" }),
      ev({
        kind: "tool_call",
        agent_id: "worker-1",
        text: "exec({})",
        tool_call_id: "c1",
      }),
      ev({ kind: "finished", agent_id: "worker-1", err: "cancelled" }),
    ])
    const [tool] = state.agents["worker-1"].blocks.filter((b) => b.kind === "tool")
    expect(tool.tool?.pending).toBe(false)
    expect(tool.tool?.failed).toBe(true)
    expect(tool.tool?.result).toBe("cancelled")
  })

  it("marks a failed call", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "tool_result", err: "command not found", tool_call_id: "c1" }),
    ])
    const [tool] = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tool.tool?.failed).toBe(true)
    expect(tool.tool?.result).toBe("command not found")
  })

  it("keeps a result with no matching call rather than dropping it", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_result", text: "orphan", tool_call_id: "unknown" }),
    ])
    const tools = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tools).toHaveLength(1)
    expect(tools[0].tool?.result).toBe("orphan")
  })

  it("parses a call into a verb and its arguments", () => {
    expect(splitToolCall('write({"file_path":"a.md"})')).toEqual({
      name: "write",
      args: '{"file_path":"a.md"}',
    })
    expect(splitToolCall("wait_agents")).toEqual({ name: "wait_agents", args: "" })
    // an argument containing a bracket must not confuse the split
    expect(splitToolCall('grep({"pattern":"a(b)c"})').args).toBe('{"pattern":"a(b)c"}')
  })
})

describe("sub-agents", () => {
  it("tracks each agent separately and records the spawn on the manager", () => {
    const state = fold([
      ev({ kind: "user_message", text: "compare two things" }),
      ev({ kind: "spawned", agent_id: "researcher-1", role: "researcher", text: "researcher" }),
      ev({ kind: "spawned", agent_id: "reviewer-2", role: "reviewer", text: "reviewer" }),
      ev({ kind: "reasoning_delta", agent_id: "researcher-1", text: "looking" }),
      ev({ kind: "agent_message", agent_id: "researcher-1", text: "found it" }),
      ev({ kind: "finished", agent_id: "researcher-1", text: "found it" }),
      ev({ kind: "finished", agent_id: "reviewer-2", err: "timed out" }),
    ])

    expect(state.agentOrder).toEqual([MANAGER_ID, "researcher-1", "reviewer-2"])
    expect(state.agents["researcher-1"].status).toBe("done")
    expect(state.agents["reviewer-2"].status).toBe("failed")
    expect(state.agents["reviewer-2"].error).toBe("timed out")
    // the researcher's own transcript, not the manager's
    expect(state.agents["researcher-1"].blocks.map((b) => b.kind)).toEqual([
      "reasoning",
      "answer",
    ])
    // and the manager's timeline says two workers started
    const spawns = manager(state).blocks.filter((b) => b.kind === "spawn")
    expect(spawns.map((s) => s.spawn?.agentId)).toEqual(["researcher-1", "reviewer-2"])
    expect(state.turns[0].agentIds).toEqual(["researcher-1", "reviewer-2"])
  })

  it("treats a second spawn of the same id as a continuation, not a twin", () => {
    const state = fold([
      ev({ kind: "user_message", text: "compare two things" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker", text: "worker" }),
      ev({ kind: "finished", agent_id: "worker-1", err: "timed out" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker", text: "worker" }),
    ])
    expect(state.agentOrder.filter((id) => id !== MANAGER_ID)).toEqual(["worker-1"])
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.agents["worker-1"].error).toBeUndefined()
    const spawns = manager(state).blocks.filter((b) => b.kind === "spawn")
    expect(spawns).toHaveLength(1)
  })

  // A worker still marked running when the turn ends was killed by cleanup;
  // leaving it spinning forever misrepresents what happened.
  it("closes agents left running when the turn ends", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker" }),
      ev({ kind: "done", text: "finished early" }),
    ])
    expect(state.agents["worker-1"].status).toBe("done")
    expect(state.running).toBe(false)
  })

  it("marks everything failed when the turn errors", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "spawned", agent_id: "worker-1", role: "worker" }),
      ev({ kind: "error", err: "the endpoint refused the connection" }),
    ])
    expect(state.agents["worker-1"].status).toBe("failed")
    expect(state.turns[0].status).toBe("error")
    expect(state.turns[0].error).toContain("refused")
    const errors = manager(state).blocks.filter((b) => b.kind === "error")
    expect(errors[0].text).toContain("refused")
  })
})

describe("turns", () => {
  it("keeps each turn with its question and its outcome", () => {
    const state = fold([
      ev({ kind: "user_message", text: "first question" }),
      ev({ kind: "agent_message", text: "first answer" }),
      ev({ kind: "done", text: "first answer" }),
      ev({ kind: "user_message", turn_id: "tn_2", text: "second question" }),
      ev({ kind: "agent_message", turn_id: "tn_2", text: "second answer" }),
      ev({ kind: "done", turn_id: "tn_2", text: "second answer" }),
    ])
    expect(state.turns.map((t) => t.userText)).toEqual([
      "first question",
      "second question",
    ])
    expect(state.turns.map((t) => t.status)).toEqual(["done", "done"])
    // both turns are in the manager's transcript, in order
    const texts = manager(state)
      .blocks.filter((b) => b.kind === "user" || b.kind === "answer")
      .map((b) => b.text)
    expect(texts).toEqual([
      "first question",
      "first answer",
      "second question",
      "second answer",
    ])
  })

  it("shows a steer as part of the conversation", () => {
    const state = fold([
      ev({ kind: "user_message", text: "look into this" }),
      ev({ kind: "steer", text: "focus on the second part" }),
    ])
    const steer = manager(state).blocks.find((b) => b.kind === "steer")
    expect(steer?.text).toBe("focus on the second part")
  })

  it("queues a steer that the manager has not read yet", () => {
    const state = fold([
      ev({ kind: "user_message", text: "look into this" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "steer", text: "focus on the second part" }),
    ])
    const { body, queued } = splitQueuedSteers(manager(state).blocks, true)
    expect(queued.map((b) => b.text)).toEqual(["focus on the second part"])
    expect(body.some((b) => b.kind === "steer")).toBe(false)
    expect(body.some((b) => b.kind === "tool")).toBe(true)
  })

  it("keeps a steer in the body once a later model round has started", () => {
    const state = fold([
      ev({ kind: "user_message", text: "look into this" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "steer", text: "focus on the second part" }),
      ev({ kind: "agent_message", text: "adjusted" }),
    ])
    const { body, queued } = splitQueuedSteers(manager(state).blocks, true)
    expect(queued).toEqual([])
    expect(body.filter((b) => b.kind === "steer").map((b) => b.text)).toEqual([
      "focus on the second part",
    ])
  })

  it("does not treat a spawn row as having consumed the steer", () => {
    // spawn_agent finishing is the current tool, not the next model call.
    const state = fold([
      ev({ kind: "user_message", text: "look into this" }),
      ev({ kind: "tool_call", text: "spawn_agent({})", tool_call_id: "c1" }),
      ev({ kind: "steer", text: "focus on the second part" }),
      ev({ kind: "spawned", agent_id: "w1", text: "researcher" }),
    ])
    const { queued } = splitQueuedSteers(manager(state).blocks, true)
    expect(queued.map((b) => b.text)).toEqual(["focus on the second part"])
  })

  it("leaves a previous turn's steer in that turn", () => {
    const state = fold([
      ev({ kind: "user_message", text: "first request", turn_id: "tn_1" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1", turn_id: "tn_1" }),
      ev({ kind: "steer", text: "focus on the second part", turn_id: "tn_1" }),
      ev({ kind: "done", text: "ok", turn_id: "tn_1" }),
      ev({ kind: "user_message", text: "next request", turn_id: "tn_2" }),
    ])
    const { body, queued } = splitQueuedSteers(manager(state).blocks, true)
    expect(queued).toEqual([])
    expect(body.filter((b) => b.kind === "steer")).toHaveLength(1)
  })

  it("does not float steers once the turn has finished", () => {
    const state = fold([
      ev({ kind: "user_message", text: "look into this" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "steer", text: "focus on the second part" }),
      ev({ kind: "done", text: "ok" }),
    ])
    const { body, queued } = splitQueuedSteers(manager(state).blocks, false)
    expect(queued).toEqual([])
    expect(body.some((b) => b.kind === "steer")).toBe(true)
  })

  it("surfaces end-of-turn cleanup", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "cleanup", text: "stopped 1 sub-agent(s) still running" }),
    ])
    expect(manager(state).blocks.some((b) => b.kind === "notice")).toBe(true)
  })

  it("keeps the partial transcript when a crashed turn is resumed", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "agent_message", text: "partial" }),
      ev({ kind: "resumed", text: "the previous run was interrupted; continuing" }),
    ])
    expect(state.running).toBe(true)
    expect(state.turns[0]?.status).toBe("running")
    expect(manager(state).blocks.filter((b) => b.kind === "user")).toHaveLength(1)
    expect(manager(state).blocks.some((b) => b.kind === "answer" && b.text === "partial")).toBe(true)
    const notice = manager(state).blocks.find((b) => b.kind === "notice")
    expect(notice?.text).toMatch(/interrupted|continuing/)
    expect(JSON.stringify(notice)).not.toMatch(/notes\.md|summarize|look into this/)
  })

  it("marks a conversation working when resume is the first event replayed", () => {
    const state = fold([
      ev({ kind: "resumed", turn_id: "tn_1", text: "the previous run was interrupted; continuing" }),
    ])
    expect(state.running).toBe(true)
    expect(state.turns[0]?.status).toBe("running")
  })

  it("pauses at the tool-round cap until the human extends it", () => {
    const paused = fold([
      ev({ kind: "user_message", text: "look into this" }),
      ev({
        kind: "max_iterations",
        text: JSON.stringify({ limit: 200, extend_by: 200 }),
      }),
    ])
    const card = manager(paused).blocks.find((b) => b.kind === "confirm")
    expect(card?.confirm).toEqual({
      limit: 200,
      extendBy: 200,
      pending: true,
    })
    expect(JSON.stringify(card)).not.toMatch(/look into this|notes\.md|ChatModel/)

    const continued = fold(
      [ev({ kind: "max_iterations_continued", text: "continuing for another 200 tool rounds" })],
      paused,
    )
    const after = manager(continued).blocks.find((b) => b.kind === "confirm")
    expect(after?.confirm?.pending).toBe(false)
    expect(after?.confirm?.continued).toBe(true)

    const stopped = fold(
      [ev({ kind: "error", err: "stopped after 200 tool rounds" })],
      paused,
    )
    const declined = manager(stopped).blocks.find((b) => b.kind === "confirm")
    expect(declined?.confirm?.pending).toBe(false)
    expect(declined?.confirm?.continued).toBe(false)
  })
})

describe("resuming", () => {
  // lastSeq is the resume cursor. A delta carries no sequence number and must
  // not move it, or a reconnect skips the stored events around it.
  it("tracks the highest stored sequence and ignores deltas", () => {
    const state = fold([
      ev({ kind: "user_message", seq: 1, text: "hi" }),
      ev({ kind: "delta", text: "streaming" }),
      ev({ kind: "agent_message", seq: 2, text: "done" }),
      ev({ kind: "delta", text: "more streaming" }),
    ])
    expect(state.lastSeq).toBe(2)
  })

  it("is unchanged by replaying the same events onto a fresh state", () => {
    const events = [
      ev({ kind: "user_message", seq: 1, text: "hi" }),
      ev({ kind: "tool_call", seq: 2, text: "read({})", tool_call_id: "c1" }),
      ev({ kind: "tool_result", seq: 3, text: "ok", tool_call_id: "c1" }),
      ev({ kind: "agent_message", seq: 4, text: "answer" }),
      ev({ kind: "done", seq: 5, text: "answer" }),
    ]
    const once = fold(events)
    const twice = fold(events)
    expect(JSON.stringify(twice)).toBe(JSON.stringify(once))
  })

  it("does not mutate the state it was given", () => {
    const before = fold([ev({ kind: "user_message", text: "hi" })])
    const snapshot = JSON.stringify(before)
    fold([ev({ kind: "delta", text: "streaming" })], before)
    expect(JSON.stringify(before)).toBe(snapshot)
  })

  it("treats an unknown event kind as a notice rather than dropping it", () => {
    const state = fold([ev({ kind: "something_new", text: "future event" })])
    const notice = manager(state).blocks.find((b) => b.kind === "notice")
    expect(notice?.text).toBe("future event")
  })
})

describe("summarise", () => {
  it("collapses whitespace and truncates", () => {
    expect(summarise("  a\n\n  b  ")).toBe("a b")
    expect(summarise("x".repeat(200)).length).toBe(80)
    expect(summarise("x".repeat(200)).endsWith("…")).toBe(true)
  })
})

describe("complete records after streaming", () => {
  // The engine streams the thought, then persists it whole once the answer
  // starts. Both must land in one block: two "Thought" rows for one thought is
  // the bug this guards.
  it("folds the persisted thought into the streamed one", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "reasoning_delta", text: "Two passes" }),
      ev({ kind: "reasoning_delta", text: "Two passes are needed" }),
      ev({ kind: "reasoning", text: "Two passes are needed" }),
      ev({ kind: "delta", text: "Splitting" }),
      ev({ kind: "agent_message", text: "Splitting it up" }),
    ])
    const blocks = manager(state).blocks
    const thoughts = blocks.filter((b) => b.kind === "reasoning")
    expect(thoughts).toHaveLength(1)
    expect(thoughts[0].text).toBe("Two passes are needed")
    expect(thoughts[0].streaming).toBeFalsy()
    const answers = blocks.filter((b) => b.kind === "answer")
    expect(answers).toHaveLength(1)
    expect(answers[0].text).toBe("Splitting it up")
  })

  it("keeps a second round's thought separate", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "reasoning", text: "First I check" }),
      ev({ kind: "tool_call", text: "read({})", tool_call_id: "c1" }),
      ev({ kind: "tool_result", text: "ok", tool_call_id: "c1" }),
      ev({ kind: "reasoning", text: "Now I answer" }),
      ev({ kind: "agent_message", text: "Answered" }),
    ])
    const thoughts = manager(state).blocks.filter((b) => b.kind === "reasoning")
    expect(thoughts.map((b) => b.text)).toEqual(["First I check", "Now I answer"])
  })
})

describe("the progress pulse", () => {
  function beat(agents: unknown[], elapsed = 12000) {
    return ev({
      kind: "progress",
      seq: 0,
      text: JSON.stringify({ elapsed_ms: elapsed, agents }),
    })
  }

  // The pulse is what a user reads when every agent is deep inside a slow tool
  // call and nothing has streamed for a minute.
  it("carries the run's age and its live sub-agents", () => {
    const state = fold([
      ev({ kind: "user_message", text: "compare two things" }),
      ev({ kind: "spawned", agent_id: "reader-1", role: "reader" }),
      beat([{ agent_id: "reader-1", role: "reader", status: "running", elapsed_ms: 8000 }]),
    ])
    expect(state.pulse?.elapsedMs).toBe(12000)
    expect(state.pulse?.agents[0]).toMatchObject({
      agentId: "reader-1",
      status: "running",
      elapsedMs: 8000,
    })
  })

  // A pulse is a snapshot with no place in the timeline; the default branch
  // would otherwise drop a "notice" block into the conversation every few
  // seconds.
  it("leaves no trace in the transcript", () => {
    const before = fold([ev({ kind: "user_message", text: "hi" })])
    const after = fold([beat([]), beat([])], before)
    expect(after.agents[MANAGER_ID].blocks).toHaveLength(1)
    expect(after.lastSeq).toBe(before.lastSeq)
  })

  // Pulses and events are broadcast on separate paths, so one built just before
  // a sub-agent finished can land just after. Believing it would flip a finished
  // agent back to running and leave it spinning forever.
  it("cannot revive an agent that has already finished", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "spawned", agent_id: "reader-1", role: "reader" }),
      ev({ kind: "tool_call", agent_id: "reader-1", text: "read({})", tool_call_id: "c9" }),
      ev({ kind: "finished", agent_id: "reader-1", role: "reader", text: "found it" }),
      beat([{ agent_id: "reader-1", role: "reader", status: "running", elapsed_ms: 9000 }]),
    ])
    expect(state.agents["reader-1"].status).toBe("done")
    expect(state.agents["reader-1"].activity).toBe("done")
  })

  // The heartbeat's count has to follow the events, not the pulse, or it will
  // contradict the rows next to it for as long as an interval lasts.
  it("counts live sub-agents from the events, and never the manager", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "spawned", agent_id: "reader-1", role: "reader" }),
      ev({ kind: "spawned", agent_id: "writer-2", role: "writer" }),
      beat([]),
    ])
    expect(liveWorkers(state)).toBe(2)
    const after = fold([ev({ kind: "finished", agent_id: "reader-1", text: "done" })], state)
    expect(liveWorkers(after)).toBe(1)
    expect(liveWorkers(fold([ev({ kind: "done", text: "answered" })], after))).toBe(0)
  })

  it("goes away when the turn does", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      beat([]),
      ev({ kind: "done", text: "answered" }),
    ])
    expect(state.pulse).toBeUndefined()
  })

  // A malformed pulse is dropped rather than rendered: showing a turn with no
  // agents in it is worse than showing the previous pulse for another few
  // seconds.
  it("ignores a payload it cannot read", () => {
    const good = fold([ev({ kind: "user_message", text: "hi" }), beat([])])
    const after = fold([ev({ kind: "progress", seq: 0, text: "not json" })], good)
    expect(after.pulse).toBe(good.pulse)
    expect(after.agents[MANAGER_ID].blocks).toHaveLength(1)
  })
})

describe("collapseLiveEvents", () => {
  // Deltas carry the accumulated string, so a burst is losslessly the last
  // snapshot. Keeping the earlier ones would be fifty React renders for the
  // same growing string.
  it("keeps the latest snapshot per agent and kind", () => {
    const collapsed = collapseLiveEvents([
      ev({ kind: "delta", text: "H" }),
      ev({ kind: "delta", text: "He" }),
      ev({ kind: "delta", text: "Hello" }),
    ])
    expect(collapsed).toHaveLength(1)
    expect(collapsed[0].text).toBe("Hello")
  })

  it("does not reorder a tool call relative to the text around it", () => {
    const collapsed = collapseLiveEvents([
      ev({ kind: "delta", text: "Checking" }),
      ev({ kind: "tool_call", text: "read({})", tool_call_id: "c1" }),
      ev({ kind: "delta", text: "Checking now" }),
    ])
    expect(collapsed.map((e) => e.kind)).toEqual(["tool_call", "delta"])
    expect(collapsed[1].text).toBe("Checking now")
  })

  it("does not mix two agents into one window", () => {
    const collapsed = collapseLiveEvents([
      ev({ kind: "delta", agent_id: "manager", text: "m1" }),
      ev({ kind: "delta", agent_id: "worker-1", text: "w1" }),
      ev({ kind: "delta", agent_id: "manager", text: "m2" }),
    ])
    expect(collapsed.map((e) => `${e.agent_id}:${e.text}`)).toEqual([
      "worker-1:w1",
      "manager:m2",
    ])
  })

  it("keeps only the latest usage pulse in a burst", () => {
    const collapsed = collapseLiveEvents([
      ev({ kind: "usage", text: '{"context_tokens":1}' }),
      ev({ kind: "delta", text: "hi" }),
      ev({ kind: "usage", text: '{"context_tokens":9}' }),
    ])
    expect(collapsed.map((e) => e.kind)).toEqual(["delta", "usage"])
    expect(collapsed[1].text).toBe('{"context_tokens":9}')
  })
})

describe("usage pulses", () => {
  it("leave no row in the transcript", () => {
    const before = fold([ev({ kind: "user_message", text: "hi" })])
    const after = fold(
      [ev({ kind: "usage", seq: 0, text: '{"context_tokens":12}' })],
      before,
    )
    expect(after.agents[MANAGER_ID].blocks).toHaveLength(1)
    expect(after.agents[MANAGER_ID].blocks[0].kind).toBe("user")
  })
})

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

  it("notices complete, continue and cap without minting workers", () => {
    const complete = fold([ev({ kind: "goal_complete", agent_id: "manager" })])
    expect(manager(complete).blocks[0].text).toBe("Standing objective completed.")
    const continued = fold([ev({ kind: "goal_continued", text: "Continuing the standing objective." })])
    expect(manager(continued).blocks[0].text).toBe("Continuing the standing objective.")
    const capped = fold([ev({ kind: "goal_capped", text: '{"auto_turns":12,"cap":12}' })])
    expect(manager(capped).blocks[0].text).toMatch(/Stopped auto-continuing/)
    expect(capped.agentOrder).toEqual([MANAGER_ID])
    const blocked = fold([ev({ kind: "goal_blocked", text: '{"reason":"needs an external change"}' })])
    expect(manager(blocked).blocks[0].text).toMatch(/blocked/)
    const edited = fold([ev({ kind: "goal_edited", text: "keep going" })])
    expect(manager(edited).blocks[0].text).toBe("Standing objective updated.")
    const resumed = fold([ev({ kind: "goal_resumed", text: "Resuming the standing objective." })])
    expect(manager(resumed).blocks[0].text).toBe("Resuming the standing objective.")
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
    expect(state.agents["compact-summarizer"]).toBeUndefined()
  })

  it("surfaces a failed compact as the recorded error", () => {
    const state = fold([
      ev({ kind: "compacted", err: "the model returned nothing usable as a briefing" }),
    ])
    expect(manager(state).blocks[0].text).toBe(
      "the model returned nothing usable as a briefing",
    )
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
