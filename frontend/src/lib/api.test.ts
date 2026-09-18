import { afterEach, describe, expect, it, vi } from "vitest"

import { api, ApiError } from "./api"

function respond(body: unknown, init?: { status?: number }) {
  const status = init?.status ?? 200
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: "",
    json: async () => body,
  } as Response
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe("settings", () => {
  it("reads null tool lists as empty ones", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond({
          settings: {
            tools: { disabled: null, enabled: null, proxy: {} },
          },
        }),
      ),
    )

    const settings = await api.settings()

    expect(settings.tools.disabled).toEqual([])
    expect(settings.tools.enabled).toEqual([])
  })

  it("keeps the lists the server sent", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond({
          settings: {
            tools: { disabled: ["exec"], enabled: ["screenshot"], proxy: {} },
          },
        }),
      ),
    )

    const settings = await api.saveSettings({})

    expect(settings.tools.disabled).toEqual(["exec"])
    expect(settings.tools.enabled).toEqual(["screenshot"])
  })
})

describe("request", () => {
  it("surfaces the server's message and code", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond(
          { error: "turn already running", code: "busy" },
          { status: 409 },
        ),
      ),
    )

    await expect(api.meta()).rejects.toMatchObject({
      message: "turn already running",
      status: 409,
      code: "busy",
    })
    await expect(api.meta()).rejects.toBeInstanceOf(ApiError)
  })

  it("keeps a conflict's current notes so the editor can show both", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond(
          {
            error: "these notes were changed after you loaded them",
            code: "conflict",
            memory: { text: "what landed", rev: "abc", chars: 11, limit: 2200 },
          },
          { status: 409 },
        ),
      ),
    )
    await expect(api.saveMemory("pj_1", "mine", "old")).rejects.toMatchObject({
      code: "conflict",
      details: {
        memory: { text: "what landed", rev: "abc" },
      },
    })
  })

  it("falls back to the status line when the body is not JSON", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 502,
        statusText: "Bad Gateway",
        json: async () => {
          throw new Error("not json")
        },
      })),
    )

    await expect(api.meta()).rejects.toMatchObject({
      message: "502 Bad Gateway",
      status: 502,
    })
  })
})

describe("threads", () => {
  it("posts compact against the conversation", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/threads/th_1/compact")
      expect(init?.method).toBe("POST")
      return respond({
        thread: { id: "th_1", compacted: true },
        status: { running: false },
      })
    })
    vi.stubGlobal("fetch", fetch)
    const got = await api.compactThread("th_1")
    expect(got.thread.compacted).toBe(true)
  })

  it("asks the desktop shell to open a URL", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/open")
      expect(init?.method).toBe("POST")
      expect(JSON.parse(String(init?.body))).toEqual({
        url: "https://example.invalid/docs",
      })
      return respond({ opened: "https://example.invalid/docs" })
    })
    vi.stubGlobal("fetch", fetch)
    const got = await api.openURL("https://example.invalid/docs")
    expect(got.opened).toBe("https://example.invalid/docs")
  })

  it("patches a standing objective", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        expect(url).toBe("/api/threads/th_1")
        expect(init?.method).toBe("PATCH")
        expect(JSON.parse(String(init?.body))).toEqual({ goal: "keep going" })
        return respond({ thread: { id: "th_1", goal: "keep going" } })
      }),
    )
    const thread = await api.patchThread("th_1", { goal: "keep going" })
    expect(thread.goal).toBe("keep going")
  })

  it("patches an in-place goal edit and a resume", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_url: string, init?: RequestInit) => {
        const body = JSON.parse(String(init?.body)) as Record<string, unknown>
        if (body.goal_edit) {
          expect(body).toEqual({ goal: "keep going, tighter", goal_edit: true })
          return respond({ thread: { id: "th_1", goal: "keep going, tighter" } })
        }
        expect(body).toEqual({ goal_resume: true })
        return respond({ thread: { id: "th_1", goal: "keep going", running: true } })
      }),
    )
    const edited = await api.patchThread("th_1", {
      goal: "keep going, tighter",
      goal_edit: true,
    })
    expect(edited.goal).toBe("keep going, tighter")
    const resumed = await api.patchThread("th_1", { goal_resume: true })
    expect(resumed.running).toBe(true)
  })

  it("pins a dragged conversation order", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/threads/reorder?project=pj_1")
      expect(init?.method).toBe("PUT")
      expect(JSON.parse(String(init?.body))).toEqual({ ids: ["th_2", "th_1"] })
      return respond({ threads: [{ id: "th_2" }, { id: "th_1" }] })
    })
    vi.stubGlobal("fetch", fetch)
    const threads = await api.reorderThreads(["th_2", "th_1"], "pj_1")
    expect(threads.map((t) => t.id)).toEqual(["th_2", "th_1"])
  })

  it("asks for a tail page of the event log", async () => {
    const fetch = vi.fn(async (url: string) => {
      expect(url).toBe("/api/threads/th_1/log?before=40&limit=24")
      return respond({ events: [{ seq: 12 }], has_more: true })
    })
    vi.stubGlobal("fetch", fetch)
    const page = await api.threadLog("th_1", { before: 40, limit: 24 })
    expect(page.has_more).toBe(true)
    expect(page.events[0]?.seq).toBe(12)
  })

  it("asks for one worker's stored log", async () => {
    const fetch = vi.fn(async (url: string) => {
      expect(url).toBe("/api/threads/th_1/agents/worker-1/log")
      return respond({ events: [{ seq: 6, kind: "tool_call" }] })
    })
    vi.stubGlobal("fetch", fetch)
    const page = await api.agentLog("th_1", "worker-1")
    expect(page.events[0]?.seq).toBe(6)
  })

  it("posts answers for a waiting question", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        expect(url).toBe("/api/threads/th_1/answers")
        expect(init?.method).toBe("POST")
        expect(JSON.parse(String(init?.body))).toEqual({
          call_id: "tc_1",
          answers: { approach: { answers: ["Prefer the safer path"] } },
        })
        return respond({ answered: true }, { status: 202 })
      }),
    )
    const got = await api.answerTurn("th_1", {
      call_id: "tc_1",
      answers: { approach: { answers: ["Prefer the safer path"] } },
    })
    expect(got.answered).toBe(true)
  })

  it("starts executing an accepted plan", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        expect(url).toBe("/api/threads/th_1/plan/implement")
        expect(init?.method).toBe("POST")
        return respond({ turn: { id: "tn_1", status: "running" } }, { status: 202 })
      }),
    )
    const got = await api.implementPlan("th_1")
    expect(got.turn.id).toBe("tn_1")
  })

  it("preempts a running turn so unread steering lands now", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/threads/th_1/preempt")
      expect(init?.method).toBe("POST")
      return respond({ preempted: true }, { status: 202 })
    })
    vi.stubGlobal("fetch", fetch)
    const got = await api.preempt("th_1")
    expect(got.preempted).toBe(true)
  })

  it("retracts one unread steer by seq", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/threads/th_1/steers/12")
      expect(init?.method).toBe("DELETE")
      return { ok: true, status: 204, statusText: "No Content", json: async () => ({}) }
    })
    vi.stubGlobal("fetch", fetch)
    await expect(api.retractSteer("th_1", 12)).resolves.toBeUndefined()
  })

  it("pins a dragged project order", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/projects/reorder")
      expect(init?.method).toBe("PUT")
      expect(JSON.parse(String(init?.body))).toEqual({ ids: ["pj_b", "pj_a"] })
      return respond({ projects: [{ id: "pj_b" }, { id: "pj_a" }] })
    })
    vi.stubGlobal("fetch", fetch)
    const projects = await api.reorderProjects(["pj_b", "pj_a"])
    expect(projects.map((p) => p.id)).toEqual(["pj_b", "pj_a"])
  })
})
