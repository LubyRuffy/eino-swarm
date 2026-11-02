import { beforeEach, describe, expect, it, vi } from "vitest"
import { useNuwa } from "./nuwa"
import { nuwaAPI, subscribeNuwa } from "@/lib/nuwa-api"
import type { NuwaSnapshot } from "@/lib/nuwa"

vi.mock("@/lib/nuwa-api", () => ({ nuwaAPI: { snapshot: vi.fn() }, subscribeNuwa: vi.fn() }))

const snapshot = (project_id: string, revision: number): NuwaSnapshot => ({ project_id, revision, nodes: [], workers: [] })
let handlers: Parameters<typeof subscribeNuwa>[1]
const close = vi.fn()

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(subscribeNuwa).mockImplementation((_id, next) => { handlers = next; return close })
  vi.mocked(nuwaAPI.snapshot).mockResolvedValue(snapshot("a", 1))
})

describe("Project collaboration snapshots", () => {
  it("rejects an older A visit after navigating A → B → A", async () => {
    let finish!: (value: NuwaSnapshot) => void
    vi.mocked(nuwaAPI.snapshot).mockReturnValueOnce(new Promise((resolve) => { finish = resolve }))
    const stopA = useNuwa.getState().connect("a")
    stopA()
    useNuwa.getState().connect("b")()
    useNuwa.getState().connect("a")
    await vi.waitFor(() => expect(useNuwa.getState().snapshot?.revision).toBe(1))
    finish(snapshot("a", 99))
    await Promise.resolve()
    expect(useNuwa.getState().snapshot?.revision).toBe(1)
    expect(close).toHaveBeenCalledTimes(2)
  })

  it("coalesces duplicate revisions and refreshes on reconnect", async () => {
    const stop = useNuwa.getState().connect("a")
    await vi.waitFor(() => expect(useNuwa.getState().snapshot?.revision).toBe(1))
    handlers.onRevision(1)
    expect(nuwaAPI.snapshot).toHaveBeenCalledTimes(1)
    vi.mocked(nuwaAPI.snapshot).mockResolvedValue(snapshot("a", 2))
    handlers.onRevision(2)
    await vi.waitFor(() => expect(useNuwa.getState().snapshot?.revision).toBe(2))
    handlers.onError()
    expect(useNuwa.getState().connected).toBe(false)
    handlers.onOpen()
    await vi.waitFor(() => expect(nuwaAPI.snapshot).toHaveBeenCalledTimes(3))
    expect(useNuwa.getState().connected).toBe(true)
    stop()
  })

  it("exposes a failed snapshot and permits a manual retry", async () => {
    vi.mocked(nuwaAPI.snapshot).mockRejectedValueOnce(new Error("read unavailable"))
    const stop = useNuwa.getState().connect("a")
    await vi.waitFor(() => expect(useNuwa.getState().error).toBe("read unavailable"))
    await useNuwa.getState().refresh()
    expect(useNuwa.getState().error).toBeUndefined()
    expect(useNuwa.getState().snapshot?.revision).toBe(1)
    stop()
  })
})
