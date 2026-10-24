import { afterEach, describe, expect, it, vi } from "vitest"
import { explicitComposerRecovery, installComposerRecoveryAttribution } from "./composer-recovery"
import { composerDraftSession } from "./composer-draft"
import { api } from "@/lib/api"
import type { Thread } from "@/lib/types"
import { useApp } from "@/store/app"
import { useProjects } from "@/store/projects"

vi.mock("@/lib/stream", () => ({ subscribeEvents: () => () => undefined }))

const original = useApp.getState()
const originalProjects = useProjects.getState()
const cleanup: Array<() => void> = []
afterEach(() => {
  while (cleanup.length) cleanup.pop()!()
  useApp.setState(original, true)
  useProjects.setState(originalProjects, true)
  vi.restoreAllMocks()
})

function attribution() {
  return (window as Window & { __zwaiRecoveryAttribution?: Record<string, unknown> }).__zwaiRecoveryAttribution
}

function thread(id: string, project_id = ""): Thread {
  return { id, title: id, project_id, provider_id: "", reasoning_effort: "", archived: false, running: false, created_at: "", last_active_at: "" }
}

function notifications() {
  const rows: Array<{ attribution: Record<string, unknown>; hasDetail: boolean }> = []
  const listen = (event: Event) => rows.push({ attribution: { ...attribution() }, hasDetail: "detail" in event })
  window.addEventListener("zwai:recovery-location", listen)
  cleanup.push(() => window.removeEventListener("zwai:recovery-location", listen))
  cleanup.push(installComposerRecoveryAttribution())
  return rows
}

describe("Native recovery attribution", () => {
  it("accepts explicit matching recovery scopes only in the desktop shell", () => {
    expect(explicitComposerRecovery("?recovery_thread=th_a")).toEqual({})
    expect(explicitComposerRecovery("?shell=desktop&recovery_thread=th_a&recovery_draft=thread:th_a")).toEqual({ threadId: "th_a", draftKey: "thread:th_a" })
    expect(explicitComposerRecovery("?shell=desktop&recovery_thread=th_a&recovery_draft=thread:th_b")).toEqual({ threadId: "th_a" })
    expect(explicitComposerRecovery("?shell=desktop&recovery_project=project_a&recovery_draft=new:project_a")).toEqual({ projectId: "project_a", draftKey: "new:project_a" })
    expect(explicitComposerRecovery("?shell=desktop&recovery_thread=../other").threadId).toBeUndefined()
  })

  it("publishes current thread and real turn attribution without message contents", () => {
    useProjects.setState({ selectedId: "project_a" })
    useApp.setState({ activeId: "th_a", status: { running: true, turn_id: "tn_live" } })
    const stop = installComposerRecoveryAttribution()
    const read = () => (window as Window & { __zwaiRecoveryAttribution?: Record<string, unknown> }).__zwaiRecoveryAttribution
    expect(read()).toMatchObject({ thread_id: "th_a", turn_id: "tn_live", draft_key: "thread:th_a", draft_saved: false })
    useApp.setState({ activeId: "th_b", status: { running: false, turn_id: "tn_b" } })
    expect(read()).toMatchObject({ thread_id: "th_b", turn_id: "tn_b", draft_key: "thread:th_b" })
    expect(Object.keys(read()!)).not.toContain("text")
    stop()
  })

  it("notifies synchronously after publishing a changed location without a content payload", () => {
    useProjects.setState({ selectedId: "project_notice" })
    useApp.setState({ activeId: "th_notice_a", status: { running: true, turn_id: "tn_notice_a" }, threads: [thread("th_notice_a", "project_notice"), thread("th_notice_b")] })
    const rows = notifications()
    expect(rows).toHaveLength(1)
    expect(rows[0]).toMatchObject({ attribution: { thread_id: "th_notice_a", turn_id: "tn_notice_a", project_id: "project_notice", draft_key: "thread:th_notice_a" }, hasDetail: false })
    useApp.setState({ activeId: "th_notice_b", status: { running: false, turn_id: "tn_notice_b" } })
    expect(rows).toHaveLength(2)
    expect(rows[1].attribution).toMatchObject({ thread_id: "th_notice_b", turn_id: "tn_notice_b", draft_key: "thread:th_notice_b" })
    expect(rows[1].attribution).not.toHaveProperty("text")
    expect(rows[1].attribution).not.toHaveProperty("quotes")
    expect(rows[1].hasDetail).toBe(false)
    useApp.setState({ status: { running: false, turn_id: "tn_notice_c" } })
    expect(rows).toHaveLength(3)
    expect(rows[2].attribution.turn_id).toBe("tn_notice_c")
  })

  it("keeps draft receipts current without notifying when only draft or unrelated state changes", async () => {
    useApp.setState({ activeId: "th_draft_notice", status: { running: true, turn_id: "tn_same" } })
    vi.spyOn(api, "composerDraft").mockResolvedValue({ key: "thread:th_draft_notice", revision: 7, text: "private unsent input", quotes: [], files: [], state: "editing" })
    const rows = notifications()
    expect(rows).toHaveLength(1)
    await composerDraftSession("thread:th_draft_notice").load()
    expect(attribution()).toMatchObject({ draft_saved: true, draft_revision: 7 })
    useApp.setState({ connected: true, status: { running: false, turn_id: "tn_same" } })
    expect(rows).toHaveLength(1)
    expect(attribution()).not.toHaveProperty("text")
  })

  it("notifies project and deliberate Home changes while ignoring an unrelated project selection", () => {
    useProjects.setState({ selectedId: "project_other" })
    useApp.setState({ activeId: "th_project_notice", status: { running: false }, threads: [thread("th_project_notice", "project_owned")] })
    const rows = notifications()
    expect(rows[0].attribution.project_id).toBe("project_owned")
    useProjects.setState({ selectedId: "project_later" })
    expect(rows).toHaveLength(1)
    useApp.setState({ threads: [thread("th_project_notice", "project_moved")] })
    expect(rows).toHaveLength(2)
    expect(rows[1].attribution.project_id).toBe("project_moved")
    useApp.setState({ activeId: undefined })
    expect(rows).toHaveLength(3)
    expect(rows[2].attribution).toMatchObject({ thread_id: undefined, project_id: "project_later", draft_key: "new:project_later" })
    useProjects.setState({ selectedId: undefined })
    expect(rows).toHaveLength(4)
    expect(rows[3].attribution.draft_key).toBe("new:home")
  })

  it("removes app, project and draft subscriptions on cleanup", async () => {
    useApp.setState({ activeId: "th_stopped_notice", status: { running: false } })
    vi.spyOn(api, "composerDraft").mockResolvedValue({ key: "thread:th_stopped_notice", revision: 9, text: "", quotes: [], files: [], state: "editing" })
    const rows = notifications()
    const prior = attribution()
    cleanup.pop()!()
    useApp.setState({ activeId: "th_after_stop" })
    useProjects.setState({ selectedId: "project_after_stop" })
    await composerDraftSession("thread:th_stopped_notice").load()
    expect(attribution()).toBe(prior)
    expect(rows).toHaveLength(1)
  })

  it("publishes the actual openThread target before its asynchronous reads without an intermediate Home", async () => {
    const target = thread("th_open_notice")
    useApp.setState({ activeId: "th_prior_notice", status: { running: false }, threads: [target] })
    useProjects.setState({ loadMemory: async () => undefined, restoreReview: async () => undefined })
    vi.spyOn(api, "thread").mockResolvedValue({ thread: target, status: { running: false } })
    vi.spyOn(api, "turns").mockResolvedValue([])
    vi.spyOn(api, "followups").mockResolvedValue([])
    vi.spyOn(api, "threadLog").mockResolvedValue({ events: [], has_more: false })
    vi.spyOn(api, "schedules").mockResolvedValue({ schedules: [], unread: 0 })
    const rows = notifications()
    const opening = useApp.getState().openThread(target.id)
    expect(rows.at(-1)?.attribution.thread_id).toBe(target.id)
    await opening
    expect(rows.slice(1).every((row) => row.attribution.thread_id === target.id)).toBe(true)
  })

  it.each([
    { name: "listed loose thread", listed: true, projectId: "", beforeProject: "" },
    { name: "listed project thread", listed: true, projectId: "project_target", beforeProject: "project_target" },
    { name: "unlisted thread", listed: false, projectId: "project_target", beforeProject: undefined },
  ])("keeps $name independent of the old project while openThread detail is pending", async ({ listed, projectId, beforeProject }) => {
    const target = thread("th_delayed_target", projectId)
    useApp.setState({ activeId: "th_old_project", status: { running: false }, threads: [thread("th_old_project", "project_old"), ...(listed ? [target] : [])] })
    useProjects.setState({ selectedId: "project_old", loadMemory: async () => undefined, restoreReview: async () => undefined })
    let finishDetail!: (value: Awaited<ReturnType<typeof api.thread>>) => void
    vi.spyOn(api, "thread").mockImplementation(() => new Promise((resolve) => { finishDetail = resolve }))
    vi.spyOn(api, "turns").mockResolvedValue([])
    vi.spyOn(api, "followups").mockResolvedValue([])
    vi.spyOn(api, "threadLog").mockResolvedValue({ events: [], has_more: false })
    vi.spyOn(api, "schedules").mockResolvedValue({ schedules: [], unread: 0 })
    const rows = notifications()
    const opening = useApp.getState().openThread(target.id)
    const before = { ...attribution() }
    const selectedBeforeDetail = useProjects.getState().selectedId
    finishDetail({ thread: target, status: { running: false } })
    await opening
    expect(selectedBeforeDetail).toBe("project_old")
    expect(before).toMatchObject({ thread_id: target.id, draft_key: `thread:${target.id}` })
    expect(before.project_id).toBe(beforeProject)
    expect(attribution()?.project_id).toBe(projectId)
    expect(rows.slice(1).every((row) => row.attribution.thread_id === target.id && row.attribution.project_id !== "project_old")).toBe(true)
  })
})
