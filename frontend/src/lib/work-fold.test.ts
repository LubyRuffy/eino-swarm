import { describe, expect, it } from "vitest"

import type { Block } from "./transcript"
import { t } from "./i18n"
import {
  fileLeaf,
  foldTurnItems,
  formatWorkTicker,
  isFoldableBlock,
  isOmittedBlock,
  workFoldStats,
  workTickerFrames,
  type WorkTickerFrame,
} from "./work-fold"

function block(partial: Partial<Block> & { kind: Block["kind"] }): Block {
  return {
    id: partial.id ?? partial.kind,
    agentId: "manager",
    text: partial.text ?? "",
    turnId: "t1",
    seq: partial.seq ?? 1,
    at: "2026-01-01T00:00:00.000Z",
    ...partial,
  }
}

const user = block({ id: "u", kind: "user", text: "ask", seq: 1 })
const thought = block({
  id: "r",
  kind: "reasoning",
  text: "first pass\nthen a closer look",
  seq: 2,
})
const tool = block({
  id: "k",
  kind: "tool",
  text: "read",
  seq: 3,
  tool: {
    callId: "c1",
    name: "read",
    args: `{"file_path":"notes.md"}`,
    pending: false,
  },
})
const answer = block({ id: "a", kind: "answer", text: "here is the result", seq: 4 })

describe("isOmittedBlock", () => {
  it("drops registry bookkeeping the chat never showed", () => {
    expect(
      isOmittedBlock(
        block({
          kind: "tool",
          tool: { callId: "c", name: "spawn_agent", args: "{}", pending: false },
        }),
      ),
    ).toBe(true)
    expect(
      isOmittedBlock(
        block({
          kind: "tool",
          tool: { callId: "c", name: "close_agent", args: "{}", pending: false },
        }),
      ),
    ).toBe(true)
    expect(isOmittedBlock(block({ kind: "title", text: "named" }))).toBe(true)
    expect(isFoldableBlock(tool)).toBe(true)
  })
})

describe("foldTurnItems", () => {
  it("keeps every row in developer mode", () => {
    const items = foldTurnItems([user, thought, tool, answer], "developer")
    expect(items.map((i) => i.type)).toEqual(["block", "block", "block", "block"])
  })

  it("folds consecutive thinking and tools in front of the final answer", () => {
    const items = foldTurnItems([user, thought, tool, answer], "user")
    expect(items).toHaveLength(3)
    expect(items[0]).toEqual({ type: "block", block: user })
    expect(items[1]?.type).toBe("work")
    if (items[1]?.type !== "work") throw new Error("expected work")
    expect(items[1].blocks.map((b) => b.id)).toEqual(["r", "k"])
    expect(items[2]).toEqual({ type: "block", block: answer })
  })

  it("pulls an answer that is followed by more tools into the fold", () => {
    const mid = block({ id: "mid", kind: "answer", text: "looking", seq: 3 })
    const later = block({
      id: "k2",
      kind: "tool",
      text: "grep",
      seq: 4,
      tool: {
        callId: "c2",
        name: "grep",
        args: `{"pattern":"alpha"}`,
        pending: false,
      },
    })
    const final = block({ id: "a2", kind: "answer", text: "done", seq: 5 })
    const items = foldTurnItems([user, thought, mid, later, final], "user")
    expect(items[0]?.type).toBe("block")
    expect(items[1]?.type).toBe("work")
    if (items[1]?.type !== "work") throw new Error("expected work")
    expect(items[1].blocks.map((b) => b.id)).toEqual(["r", "mid", "k2"])
    expect(items[2]).toEqual({ type: "block", block: final })
  })

  it("keeps a live trailing answer visible so the reply is not swallowed", () => {
    const live = block({
      id: "live",
      kind: "answer",
      text: "writing",
      seq: 4,
      streaming: true,
    })
    const items = foldTurnItems([user, tool, live], "user")
    expect(items[1]?.type).toBe("work")
    expect(items[2]).toEqual({ type: "block", block: live })
  })

  it("splits the fold around a tool-round cap the human has to answer", () => {
    const cap = block({
      id: "c",
      kind: "confirm",
      text: "limit",
      seq: 3,
      confirm: { limit: 8, extendBy: 8, pending: true },
    })
    const items = foldTurnItems([user, thought, cap, tool, answer], "user")
    expect(items.map((i) => i.type)).toEqual([
      "block",
      "work",
      "block",
      "work",
      "block",
    ])
    expect(items[2]).toEqual({ type: "block", block: cap })
  })

  it("splits the fold around a question the human has to answer", () => {
    const ask = block({ id: "q", kind: "question", text: "pick", seq: 3 })
    const items = foldTurnItems([user, thought, ask, tool, answer], "user")
    expect(items.map((i) => i.type)).toEqual([
      "block",
      "work",
      "block",
      "work",
      "block",
    ])
    expect(items[2]).toEqual({ type: "block", block: ask })
  })

  it("does not invent a fold for a plain reply", () => {
    const items = foldTurnItems([user, answer], "user")
    expect(items).toEqual([
      { type: "block", block: user },
      { type: "block", block: answer },
    ])
  })

  it("skips spawn_agent so a work group is not an empty husk", () => {
    const spawnCall = block({
      id: "sc",
      kind: "tool",
      seq: 2,
      tool: { callId: "c", name: "spawn_agent", args: "{}", pending: false },
    })
    const spawn = block({
      id: "sp",
      kind: "spawn",
      seq: 3,
      spawn: { agentId: "a-1", role: "researcher" },
    })
    const items = foldTurnItems([user, spawnCall, spawn, answer], "user")
    expect(items[1]?.type).toBe("work")
    if (items[1]?.type !== "work") throw new Error("expected work")
    expect(items[1].blocks.map((b) => b.id)).toEqual(["sp"])
  })
})

describe("workFoldStats", () => {
  it("counts thoughts and tools and notices a live call", () => {
    const pending = block({
      id: "p",
      kind: "tool",
      tool: {
        callId: "c",
        name: "read",
        args: `{"file_path":"a.ts"}`,
        pending: true,
      },
    })
    expect(workFoldStats([thought, pending])).toEqual({
      thoughts: 1,
      tools: 1,
      failed: false,
      live: true,
    })
  })
})

describe("workTickerFrames", () => {
  it("marquees thinking text while a thought is still streaming", () => {
    const later = block({
      id: "r2",
      kind: "reasoning",
      text: "first pass\nthen a closer look",
      seq: 5,
      streaming: true,
    })
    const frames = workTickerFrames([tool, later], true)
    expect(frames).toHaveLength(1)
    expect(frames[0]?.kind).toBe("thinking")
    expect(frames[0]?.detail).toBe("then a closer look")
  })

  it("reads as Reading / Editing / Exec while a tool is in flight", () => {
    const read = block({
      id: "rd",
      kind: "tool",
      tool: {
        callId: "c",
        name: "read",
        args: `{"file_path":"src/lib/appearance.ts"}`,
        pending: true,
      },
    })
    expect(workTickerFrames([read], true)[0]).toMatchObject({
      kind: "reading",
      detail: "appearance.ts",
    })

    const edit = block({
      id: "ed",
      kind: "tool",
      tool: {
        callId: "c",
        name: "edit",
        args: `{"file_path":"frontend/src/index.css"}`,
        pending: true,
      },
    })
    expect(workTickerFrames([edit], true)[0]).toMatchObject({
      kind: "editing",
      detail: "index.css",
    })

    const write = block({
      id: "wr",
      kind: "tool",
      tool: {
        callId: "c",
        name: "write",
        args: `{"file_path":"notes/out.md"}`,
        pending: true,
      },
    })
    expect(workTickerFrames([write], true)[0]).toMatchObject({
      kind: "editing",
      detail: "out.md",
    })

    const exec = block({
      id: "e",
      kind: "tool",
      tool: {
        callId: "c",
        name: "exec",
        args: `{"command":"printf x"}`,
        pending: true,
      },
    })
    expect(workTickerFrames([exec], true)[0]).toMatchObject({
      kind: "exec",
      detail: "printf x",
    })
  })

  it("uses Planning next moves in the gap between model turns", () => {
    const frames = workTickerFrames([thought, tool], true)
    expect(frames).toHaveLength(1)
    expect(frames[0]?.kind).toBe("planning")
  })

  it("does not invent a live ticker once the turn is idle", () => {
    expect(workTickerFrames([thought, tool], false)).toEqual([])
    const leftover = block({
      id: "p",
      kind: "tool",
      tool: {
        callId: "c",
        name: "exec",
        args: `{"command":"sleep 30"}`,
        pending: true,
      },
    })
    expect(workTickerFrames([leftover], false)).toEqual([])
  })

  it("lets a pending tool beat a thought that is still streaming", () => {
    const liveThought = block({
      id: "r-live",
      kind: "reasoning",
      text: "still looking",
      streaming: true,
      seq: 2,
    })
    const edit = block({
      id: "ed",
      kind: "tool",
      seq: 3,
      tool: {
        callId: "c",
        name: "edit",
        args: `{"file_path":"src/lib/appearance.ts"}`,
        pending: true,
      },
    })
    expect(workTickerFrames([liveThought, edit], true)[0]).toMatchObject({
      kind: "editing",
      detail: "appearance.ts",
    })
  })
})

describe("fileLeaf", () => {
  it("keeps the basename so the ticker is a file, not a workspace path", () => {
    expect(fileLeaf("frontend/src/lib/appearance.ts")).toBe("appearance.ts")
    expect(fileLeaf("C:\\tmp\\notes.md")).toBe("notes.md")
    expect(fileLeaf("")).toBe("")
  })
})

describe("formatWorkTicker", () => {
  const en = (key: Parameters<typeof t>[1], vars?: Parameters<typeof t>[2]) =>
    t("en", key, vars)

  function frame(partial: Partial<WorkTickerFrame> & { kind: WorkTickerFrame["kind"] }): WorkTickerFrame {
    return {
      id: partial.id ?? partial.kind,
      detail: partial.detail ?? "",
      pending: partial.pending ?? true,
      failed: partial.failed ?? false,
      ...partial,
    }
  }

  it("uses Cursor's thinking / planning / file / exec lines", () => {
    expect(formatWorkTicker(frame({ kind: "thinking", detail: "" }), en)).toBe(
      "Thinking",
    )
    expect(
      formatWorkTicker(frame({ kind: "thinking", detail: "then a closer look" }), en),
    ).toBe("then a closer look")
    expect(formatWorkTicker(frame({ kind: "planning" }), en)).toBe(
      "Planning next moves",
    )
    expect(
      formatWorkTicker(frame({ kind: "editing", detail: "appearance.ts" }), en),
    ).toBe("Editing appearance.ts")
    expect(
      formatWorkTicker(frame({ kind: "reading", detail: "index.css" }), en),
    ).toBe("Reading index.css")
    expect(formatWorkTicker(frame({ kind: "exec", detail: "printf x" }), en)).toBe(
      "Exec printf x",
    )
  })
})
