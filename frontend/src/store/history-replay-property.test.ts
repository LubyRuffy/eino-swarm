import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { emptyTranscript, reduceEvents, type TranscriptState } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { dropQueued, queueEvent, type StreamSnapshot } from "./app-stream"
import { applyTail, loadAgentHistory, loadOlderHistory, resetThreadHistory } from "./thread-history"

// The fake server answers history reads from whatever it has stored at that
// moment, the way /log and /agents/:id/log read the database.
const server = vi.hoisted(() => ({ rows: [] as SwarmEvent[], pageRand: (): number => 0.5 }))
vi.mock("@/lib/api", () => ({
  api: {
    turns: async () => [],
    threadLog: async (_id: string, opts: { before?: number; limit?: number }) => {
      const older = server.rows.filter((ev) => ev.seq < (opts.before ?? Infinity))
      const size = Math.min(older.length, opts.limit ?? older.length, 1 + Math.floor(server.pageRand() * older.length))
      return { events: older.slice(older.length - size), has_more: older.length > size }
    },
    agentLog: async (_id: string, agent: string) => ({ events: server.rows.filter((ev) => ev.agent_id === agent) }),
  },
}))

// A reader sees one conversation through a live-edge page, older pages,
// worker logs and SSE, in whatever order the network delivers them, while
// another client may rewind it. Each stored row must still count exactly
// once and in its place: the screen a reader ends with has to be the screen
// a fresh open of the server's final log paints. Example-based tests pin
// known orders; this walks seeded random ones.

const ROSTER_KINDS = new Set(["spawned", "finished", "cleanup", "workers_retained", "error"])

function prng(seed: number) {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

type Rand = ReturnType<typeof prng>
const pick = <T,>(rand: Rand, items: T[]): T => items[Math.floor(rand() * items.length)]

/** What the server does, in order: store a row, or truncate from a user
 *  message because some client resent it. A rewind is broadcast, not stored. */
type Op = { ev: SwarmEvent } | { rewind: number }

/** Plausible turns of manager talk, tools, and workers that start, think in
 *  checkpoints, finish or get swept by cleanup, sometimes resumed later under
 *  the same id. Text is structural, never a sample request. */
function script(rand: Rand, rewinds: boolean): Op[] {
  const ops: Op[] = []
  let seq = 0
  const add = (turn: string, kind: string, agent: string, extra: Partial<SwarmEvent> = {}) => {
    seq++
    ops.push({
      ev: {
        thread_id: "th_p", turn_id: turn, agent_id: agent, seq, kind,
        created_at: `2026-01-01T00:${String(Math.floor(seq / 60) % 60).padStart(2, "0")}:${String(seq % 60).padStart(2, "0")}Z`, ...extra,
      },
    })
  }
  const known: string[] = []
  let users: number[] = []
  const turns = 1 + Math.floor(rand() * (rewinds ? 4 : 3))
  for (let t = 0; t < turns; t++) {
    const turn = `tn_${t}`
    add(turn, "user_message", "manager", { text: `request ${t}`, role: "user" })
    users.push(seq)
    if (rand() < 0.5) add(turn, "reasoning", "manager", { text: `plan ${t}`, message_id: `m_r${t}` })
    const workers: string[] = []
    const count = Math.floor(rand() * 3)
    for (let w = 0; w < count; w++) {
      const id = known.length > 0 && rand() < 0.3 ? pick(rand, known) : `worker-${known.length + 1}`
      if (workers.includes(id)) continue
      if (!known.includes(id)) known.push(id)
      workers.push(id)
      add(turn, "spawned", id, { role: "worker", text: `task ${id} ${t}` })
    }
    const pending = workers.map((id) => ({ id, steps: 1 + Math.floor(rand() * 4), thought: "" }))
    let call = 0
    while (pending.length > 0) {
      const i = Math.floor(rand() * pending.length)
      const w = pending[i]
      if (w.steps > 0 && rand() < 0.3) {
        // Durable checkpoints of one model output carry its text so far.
        w.thought = w.thought ? `${w.thought} more` : `think ${w.id} ${t}`
        add(turn, "reasoning", w.id, { role: "worker", text: w.thought, message_id: `m_${w.id}_think_${t}` })
        continue
      }
      if (w.steps === 0) {
        add(turn, "agent_message", w.id, { text: `result ${w.id} ${t}`, message_id: `m_${w.id}_${t}` })
        add(turn, "finished", w.id, { role: "worker", text: `result ${w.id} ${t}` })
        pending.splice(i, 1)
        continue
      }
      const id = `c_${t}_${call++}`
      add(turn, "tool_call", w.id, { role: "worker", text: `exec({"n":${call}})`, tool_call_id: id })
      add(turn, "tool_result", w.id, { role: "worker", text: `ok ${call}`, tool_call_id: id })
      w.steps--
    }
    add(turn, "agent_message", "manager", { text: `answer ${t}`, message_id: `m_a${t}` })
    if (rand() < 0.2) add(turn, "cleanup", "manager", { text: "stopped workers" })
    if (rand() < 0.2) add(turn, "error", "manager", { err: "provider failed" })
    else add(turn, "done", "manager", { text: `answer ${t}` })
    if (rewinds && t < turns - 1 && rand() < 0.4) {
      const from = pick(rand, users)
      ops.push({ rewind: from })
      users = users.filter((s) => s < from)
    }
  }
  return ops
}

/** The stored log after the first `count` server operations. */
function rowsAfter(ops: Op[], count: number): SwarmEvent[] {
  let rows: SwarmEvent[] = []
  for (const op of ops.slice(0, count)) {
    if ("ev" in op) rows.push(op.ev)
    else rows = rows.filter((ev) => ev.seq < op.rewind)
  }
  return rows
}

function describeOp(op: Op): string {
  return "ev" in op ? `${op.ev.seq}:${op.ev.kind}:${op.ev.agent_id}:${op.ev.turn_id}` : `rewind from ${op.rewind}`
}

type Reader = StreamSnapshot & {
  historyHasMore: boolean
  historyLoading: boolean
  loadOlder: () => Promise<void>
  selectedAgent?: string
  agentLogLoading?: string
  error?: string
}
let reader: Reader
let paint: FrameRequestCallback | undefined

function patch(update: Partial<Reader> | ((s: Reader) => Partial<Reader>)) {
  reader = { ...reader, ...(typeof update === "function" ? update(reader) : update) }
}

function emit(ev: SwarmEvent) {
  queueEvent(patch, () => reader, "th_p", ev)
  const pending = paint
  paint = undefined
  pending?.(0)
}

/** What a reader can see, without bookkeeping that only steers the next read.
 *  The manager's own status is not rendered; turns and `running` carry it. */
function screen(state: TranscriptState) {
  return {
    running: state.running,
    agentOrder: state.agentOrder,
    agents: Object.fromEntries(state.agentOrder.map((id) => {
      const a = state.agents[id]
      return [id, {
        status: id === "manager" ? undefined : a.status,
        blocks: a.blocks.map((b) => ({
          kind: b.kind, text: b.text, seq: b.seq, turn: b.turnId, streaming: Boolean(b.streaming),
          ...(b.tool ? { tool: { args: b.tool.args, result: b.tool.result, pending: b.tool.pending, failed: Boolean(b.tool.failed) } } : {}),
        })),
      }]
    })),
    turns: state.turns.map((t) => ({ id: t.id, status: t.status, agentIds: t.agentIds, completedSeq: t.completedSeq })),
  }
}

/** Live-only broadcasts the engine sends before a stored row. Each carries
 *  the snapshot so far and no seq; the stored row that follows supersedes it. */
function precedingBroadcasts(ev: SwarmEvent, rand: Rand): SwarmEvent[] {
  if ((ev.kind === "reasoning" || ev.kind === "agent_message") && rand() < 0.6) {
    const text = ev.text ?? ""
    const cut = Math.max(1, Math.floor(rand() * text.length))
    return [{ ...ev, seq: 0, kind: ev.kind === "reasoning" ? "reasoning_delta" : "delta", text: text.slice(0, cut) }]
  }
  if (ev.kind === "tool_call" && rand() < 0.5) return [{ ...ev, seq: 0, kind: "tool_call_delta", text: "exec(7)" }]
  if (ev.kind === "tool_result" && rand() < 0.5) return [{ ...ev, seq: 0, kind: "tool_delta", text: (ev.text ?? "").slice(0, 2) }]
  return []
}

/** One random delivery of `ops`. Returns the steps taken so a failure names
 *  the exact interleaving to replay. */
async function deliver(ops: Op[], rand: Rand): Promise<string[]> {
  const steps: string[] = []
  let stored = 1 + Math.floor(rand() * ops.length)
  while (rowsAfter(ops, stored).length === 0) stored++
  server.rows = rowsAfter(ops, stored)
  // The opening read: a live-edge page plus roster rows outside it.
  const pageSize = 1 + Math.floor(rand() * server.rows.length)
  const tail = server.rows.slice(server.rows.length - pageSize)
  const roster = server.rows.slice(0, server.rows.length - pageSize).filter((ev) => ROSTER_KINDS.has(ev.kind))
  reader.transcript = applyTail("th_p", tail, roster)
  reader.historyHasMore = server.rows.length > pageSize
  steps.push(`tail ${tail[0].seq}..${tail[tail.length - 1].seq} after op ${stored}`)
  // SSE subscribes from the tail's newest row: it carries every later
  // operation in order, and a replay overlap may repeat a row it or the
  // tail already had.
  let sent = stored
  let sentThrough = tail[tail.length - 1].seq
  for (let guard = 0; (sent < ops.length || reader.historyHasMore) && guard < 5000; guard++) {
    const r = rand()
    if (r < 0.2 && stored < ops.length) {
      stored++
      server.rows = rowsAfter(ops, stored)
      steps.push(`store op ${stored}`)
    } else if (r < 0.55 && sent < stored) {
      const op = ops[sent++]
      if ("rewind" in op) {
        emit({ thread_id: "th_p", turn_id: "", agent_id: "manager", seq: 0, kind: "rewound", text: String(op.rewind), created_at: "2026-01-01T01:00:00Z" })
        steps.push(`sse rewound ${op.rewind}`)
        continue
      }
      for (const live of precedingBroadcasts(op.ev, rand)) {
        emit(live)
        steps.push(`sse ${live.kind} ${live.message_id ?? live.tool_call_id}`)
      }
      emit(op.ev)
      sentThrough = Math.max(sentThrough, op.ev.seq)
      steps.push(`sse ${op.ev.seq}`)
    } else if (r < 0.65) {
      const seen = server.rows.filter((ev) => ev.seq >= tail[0].seq && ev.seq <= sentThrough)
      if (seen.length === 0) continue
      const again = pick(rand, seen)
      emit(again)
      steps.push(`sse-again ${again.seq}`)
    } else if (r < 0.85 && reader.historyHasMore) {
      server.pageRand = rand
      await loadOlderHistory(() => reader, patch, String)
      steps.push(`older → oldest loaded, more=${reader.historyHasMore}`)
    } else if (r < 0.95) {
      // A worker log is fetched when the reader opens a worker already on screen.
      const workers = reader.transcript.agentOrder.filter((id) => id !== "manager")
      if (workers.length === 0) continue
      const id = pick(rand, workers)
      await loadAgentHistory(() => reader, patch, String, id)
      steps.push(`agent-log ${id} at op ${stored}`)
    } else if (stored < ops.length) {
      stored++
      server.rows = rowsAfter(ops, stored)
      steps.push(`store op ${stored}`)
    }
  }
  return steps
}

beforeEach(() => {
  dropQueued()
  resetThreadHistory()
  paint = undefined
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => { paint = callback; return 1 })
  vi.stubGlobal("cancelAnimationFrame", () => { paint = undefined })
})

afterEach(() => {
  dropQueued()
  vi.unstubAllGlobals()
})

function freshReader(): Reader {
  return {
    activeId: "th_p", transcript: emptyTranscript(), status: { running: false },
    historyHasMore: true, historyLoading: false, loadOlder: async () => {},
    threads: [], followups: [], schedules: [],
    refreshFiles: async () => {}, refreshThreads: async () => {},
    refreshFollowups: async () => {}, refreshSchedules: async () => {},
  }
}

const RUNS = Number(process.env.HISTORY_PROPERTY_RUNS ?? 300)
const FIRST_SEED = Number(process.env.HISTORY_PROPERTY_SEED ?? 1)

describe("history replay property", () => {
  for (const rewinds of [false, true]) {
    it(`ends on the screen a fresh open paints, in any delivery order${rewinds ? ", across rewinds by another client" : ""}`, async () => {
      for (let seed = FIRST_SEED; seed < FIRST_SEED + RUNS; seed++) {
        dropQueued()
        resetThreadHistory()
        reader = freshReader()
        const rand = prng(seed)
        const ops = script(rand, rewinds)
        const fresh = screen(reduceEvents(emptyTranscript(), rowsAfter(ops, ops.length)))
        const steps = await deliver(ops, rand)
        const got = screen(reader.transcript)
        if (JSON.stringify(got) !== JSON.stringify(fresh)) {
          const replay = { seed, ops: ops.map(describeOp), steps }
          expect({ ...replay, screen: got }).toEqual({ ...replay, screen: fresh })
        }
      }
    })
  }
})
