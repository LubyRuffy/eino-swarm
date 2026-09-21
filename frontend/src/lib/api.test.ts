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

  it("fills missing search settings as off with no model", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond({
          settings: {
            tools: { disabled: [], enabled: [], proxy: {} },
          },
        }),
      ),
    )
    const settings = await api.settings()
    expect(settings.search).toEqual({
      embedding: false,
      embedding_provider: "",
      embedding_model: "",
    })
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

describe("schedules", () => {
  it("lists waits with status and kind on the query string", async () => {
    const fetch = vi.fn(async (url: string) => {
      expect(url).toBe("/api/schedules?status=active&kind=thread")
      return respond({
        schedules: [{ id: "sch_1", kind: "thread", status: "active" }],
        unread: 2,
      })
    })
    vi.stubGlobal("fetch", fetch)
    const got = await api.schedules({ status: "active", kind: "thread" })
    expect(got.unread).toBe(2)
    expect(got.schedules[0]?.id).toBe("sch_1")
  })

  it("posts a create body and returns the wait", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/schedules")
      expect(init?.method).toBe("POST")
      expect(JSON.parse(String(init?.body))).toEqual({
        kind: "standalone",
        title: "wake",
        prompt: "Check current state.",
        every_s: 60,
      })
      return respond({ schedule: { id: "sch_2", kind: "standalone" } }, { status: 201 })
    })
    vi.stubGlobal("fetch", fetch)
    const row = await api.createSchedule({
      kind: "standalone",
      title: "wake",
      prompt: "Check current state.",
      every_s: 60,
    })
    expect(row.id).toBe("sch_2")
  })

  it("loads one wait with its runs", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        expect(url).toBe("/api/schedules/sch_1")
        return respond({
          schedule: { id: "sch_1" },
          runs: [{ id: "srun_1", status: "findings", unread: true }],
        })
      }),
    )
    const got = await api.schedule("sch_1")
    expect(got.runs[0]?.id).toBe("srun_1")
  })

  it("patches a wait", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/schedules/sch_1")
      expect(init?.method).toBe("PATCH")
      expect(JSON.parse(String(init?.body))).toEqual({ status: "paused" })
      return respond({ schedule: { id: "sch_1", status: "paused" } })
    })
    vi.stubGlobal("fetch", fetch)
    const row = await api.patchSchedule("sch_1", { status: "paused" })
    expect(row.status).toBe("paused")
  })

  it("deletes a wait as 204", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/schedules/sch_1")
      expect(init?.method).toBe("DELETE")
      return { ok: true, status: 204, statusText: "No Content", json: async () => ({}) }
    })
    vi.stubGlobal("fetch", fetch)
    await expect(api.deleteSchedule("sch_1")).resolves.toBeUndefined()
  })

  it("fires a wait now and returns the turn", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/schedules/sch_1/run")
      expect(init?.method).toBe("POST")
      return respond({ turn: { id: "tn_1", schedule_continue: true } }, { status: 202 })
    })
    vi.stubGlobal("fetch", fetch)
    const turn = await api.runSchedule("sch_1")
    expect(turn.id).toBe("tn_1")
    expect(turn.schedule_continue).toBe(true)
  })

  it("marks a run read as 204", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      expect(url).toBe("/api/schedules/runs/srun_1/read")
      expect(init?.method).toBe("POST")
      return { ok: true, status: 204, statusText: "No Content", json: async () => ({}) }
    })
    vi.stubGlobal("fetch", fetch)
    await expect(api.markScheduleRunRead("srun_1")).resolves.toBeUndefined()
  })

  it("surfaces skipped_busy on a conflict", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        respond({ error: "the conversation is already running", code: "skipped_busy" }, { status: 409 }),
      ),
    )
    await expect(api.runSchedule("sch_1")).rejects.toMatchObject({
      status: 409,
      code: "skipped_busy",
    })
    await expect(api.runSchedule("sch_1")).rejects.toBeInstanceOf(ApiError)
  })
})

describe("remote", () => {
  it("reads status without a token field", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        expect(url).toBe("/api/remote/status")
        return respond({
          enabled: true,
          hub_url: "http://127.0.0.1:7780",
          has_token: true,
          online: false,
        })
      }),
    )
    const st = await api.remoteStatus()
    expect(st.has_token).toBe(true)
    expect(st).not.toHaveProperty("token")
  })

  it("posts an offer and lists bindings", async () => {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/api/remote/offer") {
        expect(init?.method).toBe("POST")
        return respond({
          uri: "pairlink:v1:http://127.0.0.1:7780:code:spk",
          png: "data:image/png;base64,xx",
        })
      }
      expect(url).toBe("/api/remote/bindings")
      return respond({
        bindings: [{ id: "b1", device_fp: "abcd", created_at: "", session_id: "" }],
      })
    })
    vi.stubGlobal("fetch", fetch)
    const offer = await api.remoteOffer()
    expect(offer.png.startsWith("data:image/png")).toBe(true)
    const list = await api.remoteBindings()
    expect(list).toHaveLength(1)
  })
})

describe("search", () => {
  it("asks the server with the typed query", async () => {
    const fetch = vi.fn(async () =>
      respond({ query: "unique-body", embedding: false, hits: [] }),
    )
    vi.stubGlobal("fetch", fetch)
    const out = await api.search("unique-body")
    expect(out.hits).toEqual([])
    expect(fetch).toHaveBeenCalledWith(
      "/api/search?q=unique-body",
      expect.anything(),
    )
  })

  it("forwards a limit when the caller asks", async () => {
    const fetch = vi.fn(async () =>
      respond({ query: "alpha", embedding: false, hits: [] }),
    )
    vi.stubGlobal("fetch", fetch)
    await api.search("alpha", 5)
    expect(fetch).toHaveBeenCalledWith(
      "/api/search?q=alpha&limit=5",
      expect.anything(),
    )
  })
})
