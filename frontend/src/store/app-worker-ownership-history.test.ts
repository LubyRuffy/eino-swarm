import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "./app"
import { useProjects } from "./projects"
import { historyOldestSeq } from "./thread-history"
import type { SwarmEvent } from "@/lib/types"

const fake = vi.hoisted(() => ({
  tail: [] as SwarmEvent[], roster: [] as SwarmEvent[], older: [] as SwarmEvent[],
  onEvent: undefined as ((ev: SwarmEvent) => void) | undefined,
}))

vi.mock("@/lib/api", () => {
  const thread = () => ({ id: "thread", title: "Assigned work", provider_id: "default", archived: false,
    created_at: "2026-10-21T00:00:00Z", last_active_at: "2026-10-21T00:00:00Z", running: false })
  return { api: {
    meta: async () => ({ mode: "web", configured: true, capabilities: {} }),
    models: async () => ({ models: [], default: "default", mock: true }),
    threads: async () => [thread()], projects: async () => [],
    thread: async () => ({ thread: thread(), status: { running: false, workers: 1 } }),
    turns: async () => [],
    threadLog: async (_id: string, opts?: { before?: number }) => opts?.before
      ? { events: fake.older, has_more: false }
      : { events: fake.tail, roster: fake.roster, has_more: true },
    files: async () => ({ workspace: "/tmp/ws", files: [] }),
    followups: async () => [], schedules: async () => ({ schedules: [], unread: 0 }),
  } }
})

vi.mock("@/lib/stream", () => ({
  subscribeEvents: (_id: string, handlers: { onEvent: (ev: SwarmEvent) => void; onReady?: (p: unknown) => void }) => {
    fake.onEvent = handlers.onEvent
    handlers.onReady?.({ status: { running: false, workers: 1 } })
    return () => undefined
  },
}))

function ev(kind: string, seq: number, patch: Partial<SwarmEvent> = {}): SwarmEvent {
  return { kind, seq, thread_id: "thread", turn_id: "check", agent_id: "manager",
    created_at: "2026-10-21T09:00:00Z", ...patch }
}

const spawned = ev("spawned", 2, { agent_id: "worker-1", role: "verifier" })
const receipt = ev("workers_retained", 5, { text: JSON.stringify({ reason: "scheduled_check_failed",
  owner_kind: "thread_wake", owner_id: "wake-1", worker_ids: ["worker-1"] }) })
const failed = ev("error", 6, { err: "scheduled check did not execute" })
const later = ev("agent_message", 100, { agent_id: "worker-1", text: "Verified the next result." })

beforeEach(() => {
  fake.tail = []
  fake.roster = []
  fake.older = []
  fake.onEvent = undefined
  useApp.setState({ threads: [], activeId: undefined, status: { running: false }, followups: [], error: undefined,
    historyHasMore: false, historyLoading: false, selectedAgent: undefined, agentLogLoading: undefined })
  useProjects.setState({ projects: [], selectedId: undefined, memory: undefined, memoryProjectId: undefined,
    error: undefined, reviewing: false, reviewHint: undefined, pendingReviewTurnId: undefined })
})

async function open() {
  await useApp.getState().boot()
  await useApp.getState().openThread("thread")
  return useApp.getState().transcript
}

describe("worker ownership outside the history viewport", () => {
  it("retains a roster worker when the tail contains only its manager's terminal error", async () => {
    fake.tail = [failed]
    fake.roster = [spawned, receipt]
    const state = await open()
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.turns.find((turn) => turn.id === "check")?.status).toBe("error")
    expect(state.agents.manager.blocks.filter((block) => block.kind === "error")).toHaveLength(1)
    expect(historyOldestSeq()).toBe(6)
    fake.onEvent?.(receipt)
    fake.onEvent?.(failed)
    expect(useApp.getState().transcript.agents["worker-1"].status).toBe("running")
  })

  it("keeps a retained worker running when both receipt and error are outside the tail", async () => {
    fake.tail = [later]
    fake.roster = [spawned, receipt, failed]
    const state = await open()
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.agents["worker-1"].blocks[0].text).toBe(later.text)
    expect(state.turns.find((turn) => turn.id === "check")?.status).toBe("error")
    expect(state.agents.manager.blocks.filter((block) => block.kind === "error")).toHaveLength(0)
    expect(historyOldestSeq()).toBe(100)
  })

  it("retains ordinary error semantics when the terminal error is only in the roster", async () => {
    fake.tail = [ev("user_message", 100, { turn_id: "next-turn", text: "Inspect the saved result." })]
    fake.roster = [spawned, failed]
    const state = await open()
    expect(state.agents["worker-1"]).toMatchObject({ status: "failed", errorSource: "turn", error: failed.err })
    expect(state.agents.manager.blocks.some((block) => block.text === failed.err)).toBe(false)
  })

  it("promotes ownership and terminal events into older pages without changing worker state or paging cursors", async () => {
    fake.tail = [ev("user_message", 100, { turn_id: "next-turn", text: "Inspect current progress." })]
    fake.roster = [spawned, receipt, failed]
    fake.older = [ev("schedule_fired", 1), spawned, receipt, failed]
    await open()
    expect(historyOldestSeq()).toBe(100)
    await useApp.getState().loadOlder()
    const state = useApp.getState().transcript
    expect(state.agents["worker-1"].status).toBe("running")
    expect(state.agents.manager.blocks.filter((block) => block.kind === "error")).toHaveLength(1)
    expect(state.agents.manager.blocks.some((block) => block.text.includes("owner_kind"))).toBe(false)
    expect(historyOldestSeq()).toBe(1)
    fake.onEvent?.(failed)
    expect(useApp.getState().transcript.agents["worker-1"].status).toBe("running")
  })

  it.each(["cleanup", "finished"])("keeps later %s evidence authoritative in a roster-only history", async (kind) => {
    fake.tail = [ev("user_message", 100, { turn_id: "next-turn", text: "Inspect current progress." })]
    fake.roster = [spawned, receipt, failed,
      ev(kind, 7, { agent_id: kind === "finished" ? "worker-1" : "manager", text: "Completed." })]
    const state = await open()
    expect(state.agents["worker-1"].status).toBe(kind === "cleanup" ? "cancelled" : "done")
  })
})
