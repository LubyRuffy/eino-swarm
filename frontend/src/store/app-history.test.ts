import { beforeEach, describe, expect, it, vi } from "vitest"

import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

/** Tail/roster/agent-log tests. Kept out of app.test.ts so that file stays
 *  under 1000 lines — the mock here is only what opening a long /goal touches. */
const fake = vi.hoisted(() => ({
  logEvents: [] as Array<Record<string, unknown>>,
  logHasMore: false,
  logRoster: [] as Array<Record<string, unknown>>,
  agentLogs: {} as Record<string, Array<Record<string, unknown>>>,
  logHandler: undefined as
    | ((opts?: { before?: number; limit?: number }) =>
        | {
            events: Array<Record<string, unknown>>
            has_more: boolean
            roster?: Array<Record<string, unknown>>
          }
        | Promise<{
            events: Array<Record<string, unknown>>
            has_more: boolean
            roster?: Array<Record<string, unknown>>
          }>)
    | undefined,
  threadTitles: {} as Record<string, string>,
  subscribeSince: [] as number[],
}))

vi.mock("@/lib/api", () => {
  const thread = (id: string) => ({
    id,
    title: fake.threadTitles[id] ?? "New conversation",
    provider_id: "default",
    archived: false,
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    running: false,
  })
  return {
    api: {
      meta: async () => ({ mode: "web", configured: true, capabilities: {} }),
      models: async () => ({ models: [], default: "default", mock: true }),
      threads: async () => [thread("th_old")],
      projects: async () => [],
      thread: async (id: string) => ({
        thread: thread(id),
        status: { running: false },
      }),
      turns: async () => [],
      threadLog: async (_id: string, opts?: { before?: number; limit?: number }) => {
        if (fake.logHandler) return fake.logHandler(opts)
        return {
          events: fake.logEvents,
          has_more: fake.logHasMore,
          roster: fake.logRoster,
        }
      },
      agentLog: async (_id: string, agentId: string) => ({
        events: fake.agentLogs[agentId] ?? [],
      }),
      files: async () => ({ workspace: "/tmp/ws", files: [] }),
      followups: async () => [],
      schedules: async () => ({ schedules: [], unread: 0 }),
    },
  }
})

vi.mock("@/lib/stream", () => ({
  subscribeEvents: (
    _id: string,
    handlers: {
      onReady?: (p: unknown) => void
    },
    since = 0,
  ) => {
    fake.subscribeSince.push(since)
    handlers.onReady?.({ status: { running: false } })
    return () => undefined
  },
}))

beforeEach(() => {
  fake.logEvents = []
  fake.logHasMore = false
  fake.logRoster = []
  fake.agentLogs = {}
  fake.logHandler = undefined
  fake.threadTitles = {}
  fake.subscribeSince.length = 0
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    followups: [],
    error: undefined,
    historyHasMore: false,
    historyLoading: false,
    selectedAgent: undefined,
    agentLogLoading: undefined,
  })
  useProjects.setState({
    projects: [],
    selectedId: undefined,
    memory: undefined,
    memoryProjectId: undefined,
    error: undefined,
    reviewing: false,
    reviewHint: undefined,
    pendingReviewTurnId: undefined,
  })
})

describe("opening a long conversation", () => {
  it("pages past a roster of Started rows so the chat is not a wall of agents", async () => {
    const at = new Date().toISOString()
    fake.logHandler = (opts) => {
      if (opts?.before === 40) {
        return {
          events: [
            {
              thread_id: "th_old",
              turn_id: "tn_a",
              seq: 2,
              kind: "user_message",
              agent_id: "manager",
              text: "look into the mapping",
              created_at: at,
            },
          ],
          has_more: false,
        }
      }
      return {
        events: [
          {
            thread_id: "th_old",
            turn_id: "tn_a",
            seq: 40,
            kind: "tool_call",
            agent_id: "worker-1",
            role: "worker",
            text: "exec({})",
            tool_call_id: "c1",
            created_at: at,
          },
        ],
        has_more: true,
        roster: [
          {
            thread_id: "th_old",
            turn_id: "tn_a",
            seq: 4,
            kind: "spawned",
            agent_id: "worker-1",
            role: "worker",
            created_at: at,
          },
        ],
      }
    }
    await useApp.getState().boot()
    const blocks = useApp.getState().transcript.agents.manager?.blocks ?? []
    expect(blocks.some((b) => b.kind === "user" && b.text === "look into the mapping")).toBe(
      true,
    )
    expect(blocks.filter((b) => b.kind === "spawn")).toHaveLength(0)
    expect(useApp.getState().transcript.agents["worker-1"]?.status).toBe("running")
  })

  it("loads a worker's tools when the Agents tab opens someone off the live edge", async () => {
    const at = new Date().toISOString()
    fake.logHasMore = true
    fake.logEvents = [
      {
        thread_id: "th_old",
        turn_id: "tn_b",
        seq: 40,
        kind: "user_message",
        agent_id: "manager",
        text: "keep going",
        created_at: at,
      },
    ]
    fake.logRoster = [
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 4,
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        created_at: at,
      },
    ]
    fake.agentLogs["worker-1"] = [
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 6,
        kind: "tool_call",
        agent_id: "worker-1",
        role: "worker",
        text: "exec({})",
        tool_call_id: "c1",
        created_at: at,
      },
    ]
    await useApp.getState().boot()
    await useApp.getState().selectAgent("worker-1")
    expect(useApp.getState().transcript.agents["worker-1"]?.blocks).toHaveLength(1)
    expect(useApp.getState().historyHasMore).toBe(true)
    expect(JSON.stringify(useApp.getState().transcript)).not.toMatch(
      /notes\.md|summarize|look into this/,
    )
  })

  it("still fetches a worker log when paging said the log was complete but the pane is empty", async () => {
    const at = new Date().toISOString()
    fake.logHasMore = false
    fake.logEvents = [
      {
        thread_id: "th_old",
        turn_id: "tn_b",
        seq: 40,
        kind: "user_message",
        agent_id: "manager",
        text: "keep going",
        created_at: at,
      },
    ]
    fake.logRoster = [
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 4,
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        created_at: at,
      },
    ]
    fake.agentLogs["worker-1"] = [
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 6,
        kind: "tool_call",
        agent_id: "worker-1",
        role: "worker",
        text: "exec({})",
        tool_call_id: "c1",
        created_at: at,
      },
    ]
    await useApp.getState().boot()
    expect(useApp.getState().transcript.agents["worker-1"]?.blocks ?? []).toHaveLength(0)
    await useApp.getState().selectAgent("worker-1")
    expect(useApp.getState().transcript.agents["worker-1"]?.blocks).toHaveLength(1)
    expect(JSON.stringify(useApp.getState().transcript)).not.toMatch(
      /notes\.md|summarize|look into this/,
    )
  })
})

describe("openThread", () => {
  it("paints the tail then resumes the stream after that seq", async () => {
    fake.logEvents = [
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 9,
        kind: "user_message",
        agent_id: "manager",
        text: "latest",
        created_at: new Date().toISOString(),
      },
    ]
    fake.logHasMore = true
    await useApp.getState().boot()
    expect(useApp.getState().loaded).toBe(true)
    expect(useApp.getState().historyHasMore).toBe(true)
    expect(useApp.getState().transcript.agents.manager?.blocks[0]?.text).toBe("latest")
    expect(fake.subscribeSince.at(-1)).toBe(9)
  })

  it("puts a minted findings conversation into Recents so the title bar can change", async () => {
    await useApp.getState().boot()
    expect(useApp.getState().threads.map((t) => t.id)).toEqual(["th_old"])
    fake.threadTitles.th_minted = "periodic check"
    await useApp.getState().openThread("th_minted")
    expect(useApp.getState().activeId).toBe("th_minted")
    expect(useApp.getState().threads[0]).toMatchObject({
      id: "th_minted",
      title: "periodic check",
    })
  })

  it("pages older events above the tail", async () => {
    fake.logEvents = [
      {
        thread_id: "th_old",
        turn_id: "tn_b",
        seq: 9,
        kind: "user_message",
        agent_id: "manager",
        text: "later",
        created_at: new Date().toISOString(),
      },
    ]
    fake.logHasMore = true
    await useApp.getState().boot()
    fake.logEvents = [
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 2,
        kind: "user_message",
        agent_id: "manager",
        text: "earlier",
        created_at: new Date().toISOString(),
      },
    ]
    fake.logHasMore = false
    await useApp.getState().loadOlder(400)
    expect(
      useApp.getState().transcript.agents.manager?.blocks.map((b) => b.text),
    ).toEqual(["earlier", "later"])
    expect(useApp.getState().historyHasMore).toBe(false)
  })

  it("pages past a worker-only tail so a running conversation is not a blank pane", async () => {
    const at = new Date().toISOString()
    fake.logHandler = (opts) => {
      if (opts?.before === 40) {
        return {
          events: [
            {
              thread_id: "th_old",
              turn_id: "tn_a",
              seq: 2,
              kind: "user_message",
              agent_id: "manager",
              text: "look into the mapping",
              created_at: at,
            },
          ],
          has_more: false,
        }
      }
      return {
        events: [
          {
            thread_id: "th_old",
            turn_id: "tn_a",
            seq: 40,
            kind: "tool_call",
            agent_id: "worker-1",
            role: "worker",
            text: "exec({})",
            tool_call_id: "c1",
            created_at: at,
          },
        ],
        has_more: true,
      }
    }
    await useApp.getState().boot()
    const blocks = useApp.getState().transcript.agents.manager?.blocks ?? []
    expect(blocks.some((b) => b.kind === "user" && b.text === "look into the mapping")).toBe(
      true,
    )
    expect(useApp.getState().transcript.agents["worker-1"]?.blocks).toHaveLength(1)
    expect(useApp.getState().loaded).toBe(true)
    expect(fake.subscribeSince.at(-1)).toBe(40)
  })

  it("keeps old workers when the live-edge page is only manager tools", async () => {
    const at = new Date().toISOString()
    fake.logEvents = [
      {
        thread_id: "th_old",
        turn_id: "tn_b",
        seq: 40,
        kind: "tool_call",
        agent_id: "manager",
        text: "exec({})",
        tool_call_id: "c9",
        created_at: at,
      },
    ]
    fake.logHasMore = true
    fake.logRoster = [
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 4,
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        text: "do the assigned work",
        created_at: at,
      },
      {
        thread_id: "th_old",
        turn_id: "tn_a",
        seq: 9,
        kind: "finished",
        agent_id: "worker-1",
        role: "worker",
        text: "done",
        created_at: at,
      },
    ]
    await useApp.getState().boot()
    expect(useApp.getState().transcript.agents["worker-1"]?.status).toBe("done")
    expect(useApp.getState().transcript.agentOrder).toContain("worker-1")
    expect(useApp.getState().historyHasMore).toBe(true)
    expect(fake.subscribeSince.at(-1)).toBe(40)
    expect(
      useApp.getState().transcript.agents.manager?.blocks.filter((b) => b.kind === "spawn"),
    ).toEqual([])
    expect(JSON.stringify(useApp.getState().transcript)).not.toMatch(
      /notes\.md|summarize|look into this/,
    )
  })
})

describe("loadUntilTurn", () => {
  it("pages until a jump target still above the tail exists", async () => {
    const at = new Date().toISOString()
    const limits: number[] = []
    fake.logHandler = (opts) => {
      if (opts?.before) limits.push(opts.limit ?? 0)
      if (opts?.before === 40) {
        return {
          events: [
            {
              thread_id: "th_old",
              turn_id: "tn_a",
              seq: 2,
              kind: "user_message",
              agent_id: "manager",
              text: "earlier task",
              created_at: at,
            },
          ],
          has_more: false,
        }
      }
      return {
        events: [
          {
            thread_id: "th_old",
            turn_id: "tn_b",
            seq: 40,
            kind: "user_message",
            agent_id: "manager",
            text: "later task",
            created_at: at,
          },
        ],
        has_more: true,
      }
    }
    await useApp.getState().boot()
    expect(
      useApp.getState().transcript.agents.manager?.blocks.some((b) => b.turnId === "tn_a"),
    ).toBe(false)
    expect(await useApp.getState().loadUntilTurn("tn_a", 400)).toBe(true)
    expect(
      useApp.getState().transcript.agents.manager?.blocks.some(
        (b) => b.kind === "user" && b.turnId === "tn_a",
      ),
    ).toBe(true)
    expect(limits.at(-1)).toBe(200)
  })

  it("waits for an in-flight sentinel page instead of treating busy history as the end", async () => {
    const at = new Date().toISOString()
    let resume!: () => void
    const hold = new Promise<void>((resolve) => {
      resume = resolve
    })
    let n = 0
    fake.logHandler = (opts) => {
      n += 1
      if (!opts?.before) {
        return {
          events: [
            {
              thread_id: "th_old",
              turn_id: "tn_b",
              seq: 40,
              kind: "user_message",
              agent_id: "manager",
              text: "later task",
              created_at: at,
            },
          ],
          has_more: true,
        }
      }
      if (n === 2) {
        return hold.then(() => ({
          events: [
            {
              thread_id: "th_old",
              turn_id: "tn_b",
              seq: 20,
              kind: "tool_call",
              agent_id: "manager",
              text: "exec({})",
              tool_call_id: "c1",
              created_at: at,
            },
          ],
          has_more: true,
        }))
      }
      return {
        events: [
          {
            thread_id: "th_old",
            turn_id: "tn_a",
            seq: 2,
            kind: "user_message",
            agent_id: "manager",
            text: "earlier task",
            created_at: at,
          },
        ],
        has_more: false,
      }
    }
    await useApp.getState().boot()
    const sentinel = useApp.getState().loadOlder(400)
    const jump = useApp.getState().loadUntilTurn("tn_a", 400)
    resume()
    await sentinel
    expect(await jump).toBe(true)
    expect(
      useApp.getState().transcript.agents.manager?.blocks.some(
        (b) => b.kind === "user" && b.turnId === "tn_a",
      ),
    ).toBe(true)
  })
})
