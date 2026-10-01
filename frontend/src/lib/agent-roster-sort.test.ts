import { describe, expect, it } from "vitest"

import {
  defaultAgentRosterSort,
  sortAgentIds,
  type AgentRosterSort,
} from "./agent-roster-sort"
import type { AgentState, Block } from "./transcript"

function worker(id: string, partial: Partial<AgentState> = {}): AgentState {
  return {
    role: id,
    status: "done",
    activity: "",
    blocks: [],
    ...partial,
    id,
  }
}

function at(iso: string): Block {
  return {
    id: iso,
    kind: "answer",
    agentId: "x",
    text: "",
    turnId: "t",
    seq: 1,
    at: iso,
  }
}

function order(
  ids: string[],
  rows: AgentState[],
  sort: AgentRosterSort,
  locale?: string,
): string[] {
  const agents: Record<string, AgentState> = {}
  for (const row of rows) agents[row.id] = row
  return sortAgentIds(ids, agents, sort, locale)
}

describe("sortAgentIds", () => {
  const early = "2020-01-01T00:00:00.000Z"
  const mid = "2020-01-02T00:00:00.000Z"
  const late = "2020-01-03T00:00:00.000Z"

  it("opens with the most recently active worker first", () => {
    expect(defaultAgentRosterSort).toEqual({ key: "updated", dir: "desc" })
    const rows = [
      worker("quiet", { startedAt: early, endedAt: mid }),
      worker("fresh", { startedAt: early, endedAt: late }),
    ]
    expect(order(["quiet", "fresh"], rows, defaultAgentRosterSort)).toEqual([
      "fresh",
      "quiet",
    ])
  })

  it("treats a later block as newer than an older finish", () => {
    // A resume clears endedAt and keeps the original start. The new line
    // is the last activity; the birth time must not follow it.
    const resumed = worker("resumed", {
      startedAt: early,
      blocks: [at(late)],
    })
    const parked = worker("parked", { startedAt: mid, endedAt: mid })
    const rows = [parked, resumed]
    expect(order(["parked", "resumed"], rows, { key: "updated", dir: "desc" })).toEqual([
      "resumed",
      "parked",
    ])
    expect(order(["parked", "resumed"], rows, { key: "created", dir: "asc" })).toEqual([
      "resumed",
      "parked",
    ])
  })

  it("uses the earliest evidence when the spawn time was not stored", () => {
    const rows = [
      worker("late-only", { blocks: [at(late), at(mid)] }),
      worker("born", { startedAt: early, blocks: [at(late)] }),
    ]
    expect(order(["late-only", "born"], rows, { key: "created", dir: "asc" })).toEqual([
      "born",
      "late-only",
    ])
  })

  it("keeps a worker with no clock at the bottom either way", () => {
    const rows = [worker("blank"), worker("known", { endedAt: mid })]
    expect(order(["blank", "known"], rows, { key: "updated", dir: "desc" })).toEqual([
      "known",
      "blank",
    ])
    expect(order(["blank", "known"], rows, { key: "updated", dir: "asc" })).toEqual([
      "known",
      "blank",
    ])
    expect(order(["blank", "known"], rows, { key: "created", dir: "asc" })).toEqual([
      "known",
      "blank",
    ])
  })

  it("keeps spawn order when two workers share a timestamp", () => {
    const rows = [
      worker("first", { endedAt: mid }),
      worker("second", { endedAt: mid }),
    ]
    expect(order(["first", "second"], rows, { key: "updated", dir: "desc" })).toEqual([
      "first",
      "second",
    ])
    expect(order(["second", "first"], rows, { key: "updated", dir: "asc" })).toEqual([
      "second",
      "first",
    ])
  })

  it("orders the label on the row, with 2 before 10", () => {
    const rows = [worker("worker-10"), worker("worker-2")]
    expect(order(["worker-10", "worker-2"], rows, { key: "name", dir: "asc" })).toEqual([
      "worker-2",
      "worker-10",
    ])
    expect(order(["worker-2", "worker-10"], rows, { key: "name", dir: "desc" })).toEqual([
      "worker-10",
      "worker-2",
    ])
  })

  it("sorts the id beside the job, not the job alone", () => {
    const rows = [
      worker("z-9", { role: "m" }),
      worker("a-9", { role: "m" }),
    ]
    expect(order(["z-9", "a-9"], rows, { key: "name", dir: "asc" })).toEqual(["a-9", "z-9"])
  })

  it("orders names in the reader's language", () => {
    const rows = [worker("乙"), worker("甲")]
    expect(order(["乙", "甲"], rows, { key: "name", dir: "asc" }, "zh")).toEqual(["甲", "乙"])
  })
})
