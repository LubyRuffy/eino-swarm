import { fake } from "./app-test-fixture"
import { beforeEach, describe, expect, it, vi } from "vitest"

import { deskDest, setDeskDest } from "@/lib/desk-nav"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"
import { api } from "@/lib/api"

beforeEach(() => {
  fake.steers.length = 0
  fake.answers.length = 0
  fake.started.length = 0
  fake.enqueued.length = 0
  fake.queuedItems.length = 0
  fake.steeredFollowups.length = 0
  fake.requeued.length = 0
  fake.enqueueIdle = false
  fake.uploads.length = 0
  fake.continues.length = 0
  fake.created = 0
  fake.createDelayMs = 0
  fake.listedProjects.length = 0
  fake.listedThreads = undefined
  fake.listedFail = false
  fake.createdIn.length = 0
  fake.reviewed.length = 0
  fake.loadedMemory.length = 0
  fake.reviewCode = undefined
  fake.onEvent = undefined
  fake.discovered.length = 0
  fake.discoveryWait = undefined
  fake.phoneTimeouts = { idle: 300, first: 30 }
  fake.savedProviders = undefined
  fake.savedUI = undefined
  fake.listedModels = []
  fake.goals.length = 0
  fake.compacts.length = 0
  fake.reordered.length = 0
  fake.reorderFail = false
  fake.threadUsage = undefined
  fake.logEvents = []
  fake.logHasMore = false
  fake.logRoster = []
  fake.logHandler = undefined
  fake.subscribeSince.length = 0
  fake.metaBody = undefined
  useApp.setState({
    threads: [],
    activeId: undefined,
    status: { running: false },
    followups: [],
    error: undefined,
    historyHasMore: false,
    historyLoading: false,
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

describe("send", () => {
  it("creates a conversation when there is none", async () => {
    await useApp.getState().send("do the thing")
    expect(fake.steers).toEqual([{ id: "th_new_1", text: "do the thing" }])
  })

  it("waits for a conversation that is still being created", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(useApp.getState().activeId).toBe("th_old")

    // The click and the Enter happen before the POST comes back. Sending to
    // the conversation the user just left would run the turn out of sight.
    fake.createDelayMs = 20
    const opening = useApp.getState().newThread()
    expect(useApp.getState().creatingThread).toBe(true)
    await useApp.getState().send("in the new one")
    await opening

    expect(fake.steers).toEqual([{ id: "th_new_1", text: "in the new one" }])
    expect(useApp.getState().activeId).toBe("th_new_1")
    expect(useApp.getState().creatingThread).toBe(false)
  })

  it("releases the composer after creating a conversation fails", async () => {
    useApp.setState({ activeId: "th_old" })
    const failed = vi.spyOn(api, "createThread").mockRejectedValueOnce(new Error("creation failed"))
    try {
      expect(await useApp.getState().newThread()).toBeUndefined()
      expect(useApp.getState().creatingThread).toBe(false)
      expect(useApp.getState().activeId).toBe("th_old")
      expect(useApp.getState().error).toBe("creation failed")
    } finally { failed.mockRestore() }
  })

  it("shows the turn as running without waiting for the server to say so", async () => {
    await useApp.getState().send("go")
    expect(useApp.getState().status.running).toBe(true)
    expect(useApp.getState().status.started_at).toBeTruthy()
  })

  it("forwards pasted images on the same send as the caption", async () => {
    const images = [{ name: "clip.png", mime: "image/png", data: "AQID" }]
    await useApp.getState().send("look", images)
    expect(fake.steers).toEqual([{ id: "th_new_1", text: "look", images }])
  })

  it("injects a pasted image instead of queuing it", async () => {
    const images = [{ name: "clip.png", mime: "image/png", data: "AQID" }]
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("look", images)
    expect(fake.enqueued).toEqual([])
    expect(fake.steers).toEqual([{ id: "th_old", text: "look", images }])
  })

  it("injects a workspace file instead of queuing a text-only follow-up", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("what is this", undefined, {
      files: ["uploads/current.csv"],
    })
    expect(fake.enqueued).toEqual([])
    expect(fake.steers).toEqual([
      { id: "th_old", text: "what is this", files: ["uploads/current.csv"] },
    ])
  })

  it("queues a follow-up while a turn is running", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("after this finishes")
    expect(fake.enqueued).toEqual([{ id: "th_old", text: "after this finishes" }])
    expect(fake.steers).toEqual([])
    expect(useApp.getState().followups.map((f) => f.text)).toEqual([
      "after this finishes",
    ])
  })

  it("answers a live question instead of queuing a follow-up", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    useApp.setState({ activeId: "th_old", status: { running: true }, followups: [] })
    fake.onEvent?.({
      kind: "tool_call",
      seq: 3,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      tool_call_id: "c-ask",
      text: 'ask_user({"questions":[{"id":"approach","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]})',
    })
    expect(useApp.getState().status.awaiting_answer).toBe(true)
    expect(
      useApp.getState().threads.find((t) => t.id === "th_old")?.awaiting_answer,
    ).toBe(true)
    await useApp.getState().send("the existing approach")
    expect(fake.enqueued).toEqual([])
    expect(fake.answers).toEqual([{ id: "th_old", text: "the existing approach" }])
  })

  it("moves an edited follow-up to the back of the queue", async () => {
    useApp.setState({
      activeId: "th_old",
      followups: [
        {
          id: "fu_1",
          thread_id: "th_old",
          seq: 1,
          text: "first",
          created_at: "",
        },
        {
          id: "fu_2",
          thread_id: "th_old",
          seq: 2,
          text: "second",
          created_at: "",
        },
      ],
    })
    fake.queuedItems = [...useApp.getState().followups]
    await useApp.getState().requeueFollowup("fu_1", "first edited")
    expect(fake.requeued).toEqual([
      { id: "th_old", fid: "fu_1", text: "first edited" },
    ])
    expect(useApp.getState().followups.map((f) => f.text)).toEqual([
      "second",
      "first edited",
    ])
  })

  it("rewinds from a user message instead of steering or queuing", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [
        {
          id: "fu_1",
          thread_id: "th_old",
          seq: 1,
          text: "later",
          created_at: "",
        },
      ],
      transcript: {
        agentOrder: ["manager"],
        agents: {
          manager: {
            id: "manager",
            role: "manager",
            status: "done",
            activity: "",
            blocks: [
              {
                id: "b1",
                kind: "user",
                agentId: "manager",
                text: "first",
                turnId: "tn_1",
                seq: 1,
                at: "",
              },
              {
                id: "b2",
                kind: "user",
                agentId: "manager",
                text: "second",
                turnId: "tn_2",
                seq: 4,
                at: "",
              },
            ],
          },
        },
        turns: [
          { id: "tn_1", userText: "first", status: "done", agentIds: [] },
          { id: "tn_2", userText: "second", status: "running", agentIds: [] },
        ],
        lastSeq: 9,
        running: true,
      },
    })
    await useApp.getState().send("edited", undefined, { fromEventSeq: 4 })
    expect(fake.enqueued).toEqual([])
    expect(fake.steers).toEqual([])
    expect(fake.started).toEqual([{ id: "th_old", text: "edited", fromEventSeq: 4 }])
    expect(useApp.getState().transcript.agents.manager.blocks.map((b) => b.text)).toEqual([
      "first",
      "edited",
    ])
    expect(useApp.getState().transcript.running).toBe(true)
    expect(useApp.getState().followups).toEqual([])
  })

  it("injects immediately when asked to steer", async () => {
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("narrow it", undefined, { steer: true })
    expect(fake.steers).toEqual([{ id: "th_old", text: "narrow it" }])
    expect(fake.enqueued).toEqual([])
  })

  it("starts a turn if the queue request finds nothing running", async () => {
    fake.enqueueIdle = true
    useApp.setState({
      activeId: "th_old",
      status: { running: true },
      followups: [],
    })
    await useApp.getState().send("the race ended")
    expect(fake.steers).toEqual([{ id: "th_old", text: "the race ended" }])
  })
})

describe("upload", () => {
  it("keeps an explicit upload and send target after navigation", async () => {
    useApp.setState({ activeId: "th_other", status: { running: true }, followups: [] })
    await useApp.getState().upload([new File(["bytes"], "file.txt")], "th_original")
    const accepted = await useApp.getState().send("captured draft", undefined, { threadId: "th_original" })
    expect(accepted).toBe(true)
    expect(fake.uploads).toEqual(["th_original"])
    expect(fake.steers).toEqual([{ id: "th_original", text: "captured draft" }])
    expect(fake.enqueued).toEqual([])
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(true)
  })

  it("does not report acceptance when the HTTP acknowledgement fails", async () => {
    useApp.setState({ activeId: "th_old", status: { running: false } })
    const failed = vi.spyOn(api, "steer").mockRejectedValueOnce(new Error("connection dropped"))
    expect(await useApp.getState().send("keep this draft")).toBe(false)
    expect(useApp.getState().error).toBe("connection dropped")
    failed.mockRestore()
  })

  it("does not silently consume attachments as a text-only ask answer", async () => {
    useApp.setState({ activeId: "th_old", status: { running: true, awaiting_answer: true } })
    expect(await useApp.getState().send("answer", undefined, { files: ["uploads/kept.txt"] })).toBe(false)
    expect(fake.answers).toEqual([])
    expect(useApp.getState().error).toContain("draft was kept")
  })
  it("waits for a conversation that is still being created", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.createDelayMs = 20
    const opening = useApp.getState().newThread()
    await useApp.getState().upload([new File(["x"], "brief.txt")])
    await opening

    expect(fake.uploads).toEqual(["th_new_1"])
  })
})

describe("projects", () => {
  it("loads the memory of the chosen project without filtering the list", async () => {
    await useApp.getState().selectProject("pj_1")
    expect(useProjects.getState().selectedId).toBe("pj_1")
    expect(fake.listedProjects).not.toContain("pj_1")
    expect(fake.loadedMemory).toContain("pj_1")
  })

  // Top New conversation is Recents. A project folder's own control is
  // what lands work in that directory.
  it("starts a new conversation outside any project so it lands in Recents", async () => {
    await useApp.getState().selectProject("pj_1")
    setDeskDest("projects")
    await useApp.getState().newThread()
    expect(fake.createdIn.at(-1)).toBeUndefined()
    expect(deskDest()).toBe("chats")
  })

  // Launch is the home page. The rail stays where the reader left it;
  // only picking a row moves it.
  it("opens on the home page without selecting a conversation", async () => {
    setDeskDest("projects")
    await useApp.getState().boot()
    expect(useApp.getState().threads.length).toBeGreaterThan(0)
    expect(useApp.getState().activeId).toBeUndefined()
    expect(deskDest()).toBe("projects")
    await useApp.getState().openThread("th_old")
    expect(useApp.getState().activeId).toBe("th_old")
    expect(deskDest()).toBe("chats")
  })

  it("lets a caller override the selected project", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().newThread("pj_other")
    expect(fake.createdIn.at(-1)).toBe("pj_other")
  })

  it("pins a conversation so the sidebar can track it", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().pinThread("th_old", true)
    expect(useApp.getState().threads.find((th) => th.id === "th_old")?.pinned).toBe(
      true,
    )
    await useApp.getState().pinThread("th_old", false)
    expect(useApp.getState().threads.find((th) => th.id === "th_old")?.pinned).toBe(
      false,
    )
  })

  it("clears the selected project without re-listing", async () => {
    await useApp.getState().selectProject("pj_1")
    await useApp.getState().selectProject(undefined)
    expect(useProjects.getState().selectedId).toBeUndefined()
  })

  it("asks for a review of the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().reviewNow()
    expect(fake.reviewed).toEqual(["th_old"])
    expect(useApp.getState().error).toBeUndefined()
    expect(useProjects.getState().reviewing).toBe(true)
    expect(useProjects.getState().pendingReviewTurnId).toBe("tn_1")
  })

  // A conversation with nothing finished in it has nothing to review. That is
  // an answer on the panel they clicked, not a banner that looks like a crash.
  it("says so on the panel when there is nothing to review", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.reviewCode = "idle"
    await useApp.getState().reviewNow()
    expect(useApp.getState().error).toBeUndefined()
    expect(useProjects.getState().reviewing).toBe(false)
    expect(useProjects.getState().reviewHint).toMatch(/nothing to review/i)
  })

  it("reports a review that failed for any other reason", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.reviewCode = "busy"
    await useApp.getState().reviewNow()
    expect(useApp.getState().error).toBe("nothing to review")
    expect(useProjects.getState().reviewing).toBe(false)
  })

  it("does not review when no conversation is open", async () => {
    await useApp.getState().reviewNow()
    expect(fake.reviewed).toEqual([])
    expect(useProjects.getState().reviewHint).toMatch(/open a conversation/i)
  })
})

describe("extendTurn", () => {
  it("answers the tool-round cap on the open conversation", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    await useApp.getState().extendTurn(true)
    expect(fake.continues).toEqual([{ id: "th_old", proceed: true }])
  })

  it("does nothing when no conversation is open", async () => {
    await useApp.getState().extendTurn(false)
    expect(fake.continues).toEqual([])
  })
})

describe("a generated conversation title", () => {
  beforeEach(() => {
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal("cancelAnimationFrame", () => undefined)
  })

  function titleEvent(patch: Record<string, unknown> = {}) {
    return {
      kind: "title",
      seq: 41,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "title-namer",
      text: "Weekly status",
      created_at: new Date().toISOString(),
      ...patch,
    }
  }

  it("renames the open conversation from the title event", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(useApp.getState().threads[0]?.title).toBe("New conversation")
    fake.onEvent?.(titleEvent())
    expect(useApp.getState().threads[0]?.title).toBe("Weekly status")
  })

  it("keeps that name when a done refresh still has the placeholder", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.onEvent?.(titleEvent())
    await useApp.getState().refreshThreads()
    expect(useApp.getState().threads[0]?.title).toBe("Weekly status")
    expect(useApp.getState().threads[0]?.title_auto).toBe(false)
  })

  it("does not rename from a namer that failed", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.onEvent?.(titleEvent({ err: "endpoint down" }))
    expect(useApp.getState().threads[0]?.title).toBe("New conversation")
  })
})

describe("refreshCatalogs", () => {
  it("keeps phone timeouts changed while endpoint discovery was pending", async () => {
    let release!: () => void
    fake.discoveryWait = new Promise<void>((done) => { release = done })
    const refresh = useApp.getState().refreshCatalogs()
    await vi.waitFor(() => expect(fake.discovered).toHaveLength(1))
    fake.phoneTimeouts = { idle: 99, first: 43 }
    release(); await refresh
    expect(fake.phoneTimeouts).toEqual({ idle: 99, first: 43 })
    const rows = fake.savedProviders as Array<Record<string, unknown>>
    expect(rows[0]).not.toHaveProperty("timeout_seconds")
    expect(rows[0]).not.toHaveProperty("first_byte_timeout_seconds")
    expect(rows[0].catalog).toEqual(["alpha", "beta"])
  })
  it("re-lists every endpoint with a URL and does not reboot the conversation", async () => {
    fake.listedModels = [
      {
        id: "default\tbeta",
        provider_id: "default",
        provider_label: "Main",
        label: "beta",
        model: "beta",
        ready: true,
      },
    ]
    await useApp.getState().refreshCatalogs()
    expect(fake.discovered).toEqual([
      { provider_id: "default", base_url: "http://endpoint.invalid/v1" },
    ])
    const saved = fake.savedProviders as Array<{
      id: string
      catalog: string[]
      api_key?: string
    }>
    expect(saved.map((p) => p.id)).toEqual(["default", "blank"])
    expect(saved[0].catalog).toEqual(["alpha", "beta"])
    expect(saved[0]).toMatchObject({ model_context: { alpha: 128000 } })
    expect(saved[0].api_key).toBeUndefined()
    expect(useApp.getState().models).toEqual(fake.listedModels)
    expect(useApp.getState().activeId).toBeUndefined()
  })
})

describe("token usage", () => {
  beforeEach(() => {
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
    vi.stubGlobal("cancelAnimationFrame", () => undefined)
  })

  it("updates the meter from a live usage event", async () => {
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    fake.onEvent?.({
      kind: "usage",
      seq: 0,
      thread_id: "th_old",
      turn_id: "tn_1",
      agent_id: "manager",
      text: JSON.stringify({
        context_tokens: 71300,
        context_window: 256000,
        turn: { prompt_tokens: 8, completion_tokens: 2, total_tokens: 10, calls: 1 },
        thread: { total_tokens: 10, calls: 1 },
      }),
      created_at: new Date().toISOString(),
    })
    expect(useApp.getState().usage?.context_tokens).toBe(71300)
    expect(useApp.getState().usage?.context_window).toBe(256000)
  })

  it("loads the snapshot when opening a conversation", async () => {
    fake.threadUsage = {
      context_tokens: 90,
      context_window: 128000,
      turn: { prompt_tokens: 90, completion_tokens: 10, total_tokens: 100, calls: 1 },
      thread: { prompt_tokens: 90, completion_tokens: 10, total_tokens: 100, calls: 1 },
    }
    await useApp.getState().boot()
    await useApp.getState().openThread("th_old")
    expect(useApp.getState().usage?.context_tokens).toBe(90)
  })
})

describe("sidebar order", () => {
  it("pins a dragged conversation order", async () => {
    useApp.setState({
      threads: [
        {
          id: "th_1",
          title: "A",
          project_id: "",
          provider_id: "default",
          reasoning_effort: "",
          archived: false,
          created_at: "",
          last_active_at: "",
          running: false,
        },
        {
          id: "th_2",
          title: "B",
          project_id: "",
          provider_id: "default",
          reasoning_effort: "",
          archived: false,
          created_at: "",
          last_active_at: "",
          running: false,
        },
      ],
    })
    await useApp.getState().reorderThreads(["th_2", "th_1"])
    expect(fake.reordered).toEqual([{ ids: ["th_2", "th_1"], projectId: undefined }])
    expect(useApp.getState().threads.map((t) => t.id)).toEqual(["th_2", "th_1"])
  })

  it("reloads the list when a reorder fails", async () => {
    fake.reorderFail = true
    useApp.setState({
      threads: [
        {
          id: "th_1",
          title: "A",
          project_id: "",
          provider_id: "default",
          reasoning_effort: "",
          archived: false,
          created_at: "",
          last_active_at: "",
          running: false,
        },
      ],
    })
    await useApp.getState().reorderThreads(["th_1"])
    expect(useApp.getState().error).toMatch(/could not pin the order/)
    expect(useApp.getState().threads.map((t) => t.id)).toEqual(["th_old"])
  })
})

describe("sidebar running", () => {
  const listed = (id: string, running = false, project_id = "pj_busy") => ({
    id,
    title: id,
    title_auto: true,
    project_id,
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: new Date().toISOString(),
    last_active_at: new Date().toISOString(),
    running,
  })

  it("keeps a running conversation marked after switching away", async () => {
    useApp.setState({
      activeId: "th_old",
      loaded: true,
      status: { running: true, started_at: new Date().toISOString() },
      threads: [listed("th_old", false, ""), listed("th_other", false, "")],
    })
    await useApp.getState().openThread("th_other")
    expect(useApp.getState().activeId).toBe("th_other")
    expect(useApp.getState().status.running).toBe(false)
    expect(useApp.getState().threads.find((t) => t.id === "th_old")?.running).toBe(
      true,
    )
  })

  it("keeps that mark after New conversation when the listing still reports it running", async () => {
    useApp.setState({
      activeId: "th_old",
      loaded: true,
      status: { running: true, started_at: new Date().toISOString() },
      threads: [listed("th_old", true)],
    })
    await useApp.getState().newThread()
    fake.listedThreads = [
      listed("th_old", true),
      listed(useApp.getState().activeId ?? "th_new", false),
    ]
    await useApp.getState().refreshThreads()
    expect(useApp.getState().activeId).not.toBe("th_old")
    expect(useApp.getState().threads.find((t) => t.id === "th_old")?.running).toBe(
      true,
    )
  })

  it("lights a background conversation from the listing without opening it", async () => {
    useApp.setState({
      activeId: "th_open",
      loaded: true,
      status: { running: false },
      threads: [listed("th_open"), listed("th_busy")],
    })
    fake.listedThreads = [listed("th_open"), listed("th_busy", true)]
    await useApp.getState().refreshThreads()
    expect(useApp.getState().threads.find((t) => t.id === "th_busy")?.running).toBe(
      true,
    )
  })

  it("idles a left conversation once the listing says it finished", async () => {
    useApp.setState({
      activeId: "th_open",
      loaded: true,
      status: { running: false },
      threads: [listed("th_open"), listed("th_old", true)],
    })
    fake.listedThreads = [listed("th_open"), listed("th_old")]
    await useApp.getState().refreshThreads()
    expect(useApp.getState().threads.find((t) => t.id === "th_old")?.running).toBe(
      false,
    )
  })

  it("keeps the open conversation marked when a listing fetch raced Enter", async () => {
    useApp.setState({
      activeId: "th_old",
      loaded: true,
      status: { running: true, started_at: new Date().toISOString() },
      threads: [listed("th_old", true, "")],
    })
    fake.listedThreads = [listed("th_old", false, "")]
    await useApp.getState().refreshThreads()
    expect(useApp.getState().threads.find((t) => t.id === "th_old")?.running).toBe(
      true,
    )
  })

  it("does not toast when a background listing fetch fails", async () => {
    fake.listedFail = true
    useApp.setState({ error: undefined })
    await useApp.getState().syncThreads()
    expect(useApp.getState().error).toBeUndefined()
  })

  it("refreshes which shells are attached on the same tick as the list", async () => {
    fake.metaBody = {
      mode: "engine",
      configured: true,
      capabilities: {},
      clients: [
        { id: "pc_a", surface: "desktop" },
        { id: "pc_b", surface: "tui" },
      ],
    }
    fake.listedThreads = []
    await useApp.getState().syncThreads()
    expect(useApp.getState().meta?.clients).toHaveLength(2)
  })
})
