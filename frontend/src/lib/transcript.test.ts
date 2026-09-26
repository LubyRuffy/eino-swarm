import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  collapseLiveEvents,
  emptyTranscript,
  lastFailedTurnError,
  liveWorkers,
  reduceEvent,
  reduceEvents,
  splitQueuedSteers,
  splitToolCall,
  summarise,
  type TranscriptState,
} from "./transcript"
import type { SwarmEvent } from "./types"

let seq = 0
function ev(partial: Partial<SwarmEvent> & { kind: string }): SwarmEvent {
  const live =
    partial.kind === "delta" ||
    partial.kind === "reasoning_delta" ||
    partial.kind === "tool_delta" ||
    partial.kind === "progress" ||
    partial.kind === "usage" ||
    partial.kind === "rewound"
  return {
    thread_id: "th_1",
    turn_id: partial.turn_id ?? "tn_1",
    seq: partial.seq ?? (live ? 0 : ++seq),
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

  // A streaming model emits the commentary, then tool_call_delta while the
  // arguments are still being written, then the sealed agent_message of that
  // same commentary (the accumulator flushes it when the call is issued).
  // Sealing the bubble on the delta leaves the sealed text above the tools
  // and the flush paints the same paragraph again underneath.
  it("keeps one commentary when tool arguments stream before the sealed message", () => {
    const text = "The service is stopped. I will check the config and bring it up."
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "delta", text }),
      ev({ kind: "tool_call_delta", text: "exec(12)", tool_call_id: "c1" }),
      ev({ kind: "tool_call_delta", text: "exec(40)", tool_call_id: "c2" }),
      ev({ kind: "agent_message", text }),
      ev({ kind: "tool_call", text: 'exec({"command":"tailscale status"})', tool_call_id: "c1" }),
      ev({ kind: "tool_call", text: 'exec({"command":"scutil --nc list"})', tool_call_id: "c2" }),
    ])
    const answers = manager(state).blocks.filter((b) => b.kind === "answer")
    expect(answers).toHaveLength(1)
    expect(answers[0].text).toBe(text)
    expect(answers[0].streaming).toBeFalsy()
    const tools = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tools).toHaveLength(2)
    const answerAt = manager(state).blocks.findIndex((b) => b.kind === "answer")
    const firstTool = manager(state).blocks.findIndex((b) => b.kind === "tool")
    expect(answerAt).toBeLessThan(firstTool)
  })

  // The coalesce timer can claim the commentary and then lose the race, so
  // the same snapshot arrives again after the tool row. A second bubble there
  // is the paragraph the user already read, painted twice.
  it("drops a commentary snapshot that arrives again after its tool calls", () => {
    const text = "The service is stopped. I will check the config and bring it up."
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "delta", text }),
      ev({ kind: "agent_message", text }),
      ev({ kind: "tool_call", text: 'exec({"command":"status"})', tool_call_id: "c1" }),
      ev({ kind: "tool_call", text: 'exec({"command":"start"})', tool_call_id: "c2" }),
      ev({ kind: "delta", text }),
    ])
    const blocks = manager(state).blocks
    expect(blocks.filter((b) => b.kind === "answer")).toHaveLength(1)
    const answerAt = blocks.findIndex((b) => b.kind === "answer")
    let lastTool = -1
    for (let i = blocks.length - 1; i >= 0; i--) {
      if (blocks[i].kind === "tool") {
        lastTool = i
        break
      }
    }
    expect(answerAt).toBeLessThan(lastTool)
  })

  // tool_call seals the open bubble before the flushed message arrives. The
  // flush has to land in that bubble, not under the tools.
  it("folds a sealed commentary into the bubble the tool call already closed", () => {
    const text = "The service is stopped. I will check the config and bring it up."
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "delta", text: "The service is stopped." }),
      ev({ kind: "tool_call", text: 'exec({"command":"status"})', tool_call_id: "c1" }),
      ev({ kind: "agent_message", text }),
    ])
    const answers = manager(state).blocks.filter((b) => b.kind === "answer")
    expect(answers).toHaveLength(1)
    expect(answers[0].text).toBe(text)
    const answerAt = manager(state).blocks.findIndex((b) => b.kind === "answer")
    const toolAt = manager(state).blocks.findIndex((b) => b.kind === "tool")
    expect(answerAt).toBeLessThan(toolAt)
  })

  // A second stored message that repeats the sentence is the model saying it
  // again. The echo guard is for a streamed snapshot, not for that.
  it("keeps a repeated sentence when a tool sits between two stored messages", () => {
    const text = "status update"
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "agent_message", text }),
      ev({ kind: "tool_call", text: 'exec({"command":"true"})', tool_call_id: "c1" }),
      ev({ kind: "agent_message", text }),
    ])
    expect(manager(state).blocks.filter((b) => b.kind === "answer").map((b) => b.text)).toEqual([
      text,
      text,
    ])
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

  it("hydrates a roster spawned worker without a Started row on the manager", () => {
    const spawned = ev({
      kind: "spawned",
      agent_id: "worker-1",
      role: "worker",
      text: "do the assigned work",
    })
    const state = reduceEvent(emptyTranscript(), spawned, "roster")
    expect(state.agents["worker-1"]?.status).toBe("running")
    expect(state.agents["worker-1"]?.instruction).toBe("do the assigned work")
    expect(manager(state)?.blocks.filter((b) => b.kind === "spawn")).toEqual([])
    expect(state.turns).toEqual([])
    expect(JSON.stringify(state)).not.toMatch(/notes\.md|summarize|look into this/)
  })

  it("still paints Started when the spawn is on the loaded page", () => {
    const spawned = ev({
      kind: "spawned",
      agent_id: "worker-1",
      role: "worker",
      text: "do the assigned work",
    })
    const state = reduceEvent(emptyTranscript(), spawned, "full")
    const spawns = manager(state).blocks.filter((b) => b.kind === "spawn")
    expect(spawns).toHaveLength(1)
    expect(spawns[0]?.spawn?.agentId).toBe("worker-1")
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
    expect(lastFailedTurnError(state.turns)).toBe("the endpoint refused the connection")
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

  it("keeps leftover sub-agents running when the turn is resumed", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        text: "do the assigned work",
      }),
      ev({ kind: "resumed", text: "the previous run was interrupted; continuing" }),
      ev({
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        text: "do the assigned work",
      }),
    ])
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.agents["worker-1"].activity).toBe("continuing")
    expect(manager(state).blocks.filter((b) => b.kind === "spawn")).toHaveLength(1)
    expect(JSON.stringify(state)).not.toMatch(/notes\.md|summarize|look into this/)
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

  it("clears a leftover tool-round prompt when the crashed turn is resumed", () => {
    const paused = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({
        kind: "max_iterations",
        text: JSON.stringify({ limit: 200, extend_by: 200 }),
      }),
    ])
    const resumed = fold(
      [ev({ kind: "resumed", text: "the previous run was interrupted; continuing" })],
      paused,
    )
    const card = manager(resumed).blocks.find((b) => b.kind === "confirm")
    expect(card?.confirm?.pending).toBe(false)
    expect(card?.confirm?.continued).toBe(true)
    expect(resumed.running).toBe(true)
  })

  it("keeps a live question open when the crashed turn is resumed", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({
        kind: "tool_call",
        tool_call_id: "c-ask",
        text: 'ask_user({"questions":[{"id":"approach","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]})',
      }),
      ev({ kind: "resumed", text: "the previous run was interrupted; continuing" }),
    ])
    const card = manager(state).blocks.find((b) => b.kind === "question")
    expect(card?.question?.pending).toBe(true)
    expect(state.running).toBe(true)
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
    expect(liveWorkers(fold([ev({ kind: "done", text: "answered" })], after))).toBe(1)
    expect(
      liveWorkers(
        fold([ev({ kind: "cleanup", text: "stopped 1 sub-agent(s) still running" })], after),
      ),
    ).toBe(0)
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
