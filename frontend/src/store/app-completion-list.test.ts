import { beforeEach, describe, expect, it, vi } from "vitest"
import type { Thread } from "@/lib/types"
import { useApp } from "./app"
const fake = vi.hoisted(() => ({ threads: vi.fn(), meta: vi.fn(async () => undefined) }))
vi.mock("@/lib/api", () => ({ api: fake, ApiError: Error }))
const old = { id: "one", completed_seq: 12, unread: true, running: false } as Thread
beforeEach(() => {
  fake.threads.mockReset()
  useApp.setState({ threads: [old], activeId: undefined, status: { running: false }, refreshSchedules: async () => {} })
})
describe("completion listing races", () => {
  it.each(["refreshThreads", "syncThreads"] as const)("%s cannot rewind a completion that advanced while fetching", async (method) => {
    let finish!: (value: Thread[]) => void
    fake.threads.mockReturnValue(new Promise((resolve) => { finish = resolve }))
    const pending = useApp.getState()[method]()
    useApp.setState({ threads: [{ ...old, completed_seq: 20 }] })
    finish([{ ...old, unread: false }])
    await pending
    expect(useApp.getState().threads[0]).toMatchObject({ completed_seq: 20, unread: true })
  })
  it("accepts a real rewind when no local completion changed during fetching", async () => {
    fake.threads.mockResolvedValue([{ ...old, completed_seq: 0, unread: false }])
    await useApp.getState().refreshThreads()
    expect(useApp.getState().threads[0]).toMatchObject({ completed_seq: 0, unread: false })
  })
})
