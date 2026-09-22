import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { openSaved, type DeviceLink } from "@/lib/client"
import { t } from "@/lib/i18n"
import {
  OpHello,
  OpList,
  OpOpen,
  OpStart,
  OpUnwatch,
  OpWatch,
  type RemoteRequest,
  type RemoteResponse,
} from "@/lib/rpc"
import { saveLink } from "@/lib/store"

vi.mock("@/lib/client", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/lib/client")>()
  return { ...actual, openSaved: vi.fn() }
})

import { App } from "./app"

function seedLink() {
  saveLink({
    hubURL: "http://127.0.0.1:9",
    ticket: "tick",
    hostPub: "pub",
    sessionID: "sid",
    fingerprint: "fp",
  })
}

describe("App boot chrome", () => {
  afterEach(() => {
    cleanup()
    vi.mocked(openSaved).mockReset()
  })

  it("does not offer an app update in the browser", () => {
    render(<App />)
    expect(screen.queryByRole("button", { name: t("update.upgrade") })).not.toBeInTheDocument()
  })

  it("shows the scan form only when this phone has never bound", () => {
    render(<App />)
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeInTheDocument()
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument()
    expect(systemBack()).toBe(false)
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeInTheDocument()
  })

  it("paints host tabs and a connecting inbox when a ticket is saved", () => {
    seedLink()
    vi.mocked(openSaved).mockReturnValue(new Promise(() => undefined))
    render(<App />)
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
    expect(screen.getByRole("status")).toHaveAttribute("aria-busy", "true")
    expect(screen.getByText(t("scan.connecting"))).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: t("scan.title") })).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: t("scan.camera") })).not.toBeInTheDocument()
  })

  it("stays off the scan form after a failed restore so a bound phone is not mistaken for unbound", async () => {
    seedLink()
    vi.mocked(openSaved).mockRejectedValue(new Error("offline"))
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(
        t("err.net.closed", { reason: "offline" }),
      )
    })
    expect(screen.getByRole("tablist")).toBeInTheDocument()
    expect(screen.queryByRole("heading", { name: t("scan.title") })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: t("scan.retry") }))
    await waitFor(() => {
      expect(vi.mocked(openSaved).mock.calls.length).toBeGreaterThanOrEqual(2)
    })
  })

  it("returns to scan after unlink from a failed restore", async () => {
    seedLink()
    vi.mocked(openSaved).mockRejectedValue(new Error("offline"))
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole("button", { name: t("home.menu") })).toBeEnabled()
    })
    fireEvent.click(screen.getByRole("button", { name: t("home.menu") }))
    fireEvent.click(screen.getByRole("menuitem", { name: t("home.unlink") }))
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeInTheDocument()
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument()
  })

  it("opens add as a side sheet over the inbox", async () => {
    seedLink()
    vi.mocked(openSaved).mockReturnValue(new Promise(() => undefined))
    render(<App />)
    openAddHost()
    expect(screen.getByRole("dialog", { name: t("home.addHost") })).toBeInTheDocument()
    expect(screen.getByRole("tablist")).toBeInTheDocument()
    expect(screen.getByText(t("scan.connecting"))).toBeInTheDocument()
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeEnabled()
  })

  it("system back closes Add a PC and stays on the inbox", async () => {
    seedLink()
    vi.mocked(openSaved).mockReturnValue(new Promise(() => undefined))
    render(<App />)
    openAddHost()
    expect(screen.getByRole("dialog", { name: t("home.addHost") })).toBeInTheDocument()
    let stayed = false
    await act(async () => {
      stayed = systemBack()
    })
    expect(stayed).toBe(true)
    expect(screen.queryByRole("dialog", { name: t("home.addHost") })).not.toBeInTheDocument()
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
    expect(systemBack()).toBe(false)
  })

  it("keeps a restore error on the inbox when Add a PC is open", async () => {
    seedLink()
    vi.mocked(openSaved).mockRejectedValue(new Error("offline"))
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(
        t("err.net.closed", { reason: "offline" }),
      )
    })
    openAddHost()
    const dialog = screen.getByRole("dialog", { name: t("home.addHost") })
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: t("scan.camera") })).toBeEnabled()
    expect(screen.getByRole("alert").textContent).toBe(
      t("err.net.closed", { reason: "offline" }),
    )
  })

  it("says the PC may be off when the socket never opens", async () => {
    seedLink()
    vi.mocked(openSaved).mockRejectedValue(new Error("ws error"))
    render(<App />)
    const down = t("err.net.down", { reason: "ws error" })
    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(down)
    })
    expect(down).not.toBe(t("err.remote", { detail: "ws error" }))
    expect(screen.getByLabelText("path=offline").className).toContain("bg-muted-foreground")
    expect(screen.getByLabelText("path=offline").className).not.toContain("--online")
  })

  it("labels a server refusal instead of a dropped socket", async () => {
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(
      hostLink({
        list: async () => ({ v: 1, id: "l", ok: false, error: "quota" }),
      }),
    )
    render(<App />)
    const remote = t("err.remote", { detail: "quota" })
    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(remote)
    })
    expect(remote).not.toBe(t("err.net.down", { reason: "quota" }))
    expect(screen.getByLabelText("path=relay").className).toContain("bg-[hsl(var(--online))]")
  })
})

describe("system back leaves a conversation for the inbox", () => {
  afterEach(() => {
    cleanup()
    vi.mocked(openSaved).mockReset()
  })

  it("pops an open conversation and finishes only from the inbox", async () => {
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(hostLink())
    render(<App />)
    const open = t("home.open", { title: "th-1" })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: open })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole("button", { name: open }))
    await waitFor(() => {
      expect(screen.getByRole("button", { name: t("thread.back") })).toBeInTheDocument()
    })
    let stayed = false
    await act(async () => {
      stayed = systemBack()
    })
    expect(stayed).toBe(true)
    await waitFor(() => {
      expect(screen.getByRole("button", { name: open })).toBeInTheDocument()
    })
    expect(screen.queryByRole("button", { name: t("thread.back") })).not.toBeInTheDocument()
    expect(systemBack()).toBe(false)
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
  })

  it("header Back returns to the inbox", async () => {
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(hostLink())
    render(<App />)
    const open = t("home.open", { title: "th-1" })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: open })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole("button", { name: open }))
    await waitFor(() => {
      expect(screen.getByRole("button", { name: t("thread.back") })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole("button", { name: t("thread.back") }))
    await waitFor(() => {
      expect(screen.getByRole("button", { name: open })).toBeInTheDocument()
    })
    expect(screen.queryByRole("heading", { name: "th-1" })).not.toBeInTheDocument()
  })

  it("returns from a resumed live conversation instead of treating it as the root", async () => {
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(
      hostLink({
        running: [{ thread_id: "th-1", title: "th-1" }],
        threads: [],
      }),
    )
    render(<App />)
    await waitFor(() => {
      expect(screen.getByRole("button", { name: t("thread.back") })).toBeInTheDocument()
    })
    let stayed = false
    await act(async () => {
      stayed = systemBack()
    })
    expect(stayed).toBe(true)
    await waitFor(() => {
      expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
    })
    expect(screen.queryByRole("button", { name: t("thread.back") })).not.toBeInTheDocument()
    expect(systemBack()).toBe(false)
  })

  it("does not paint the conversation back when open replies after Back", async () => {
    let releaseOpen: (resp: RemoteResponse) => void = () => undefined
    const openGate = new Promise<RemoteResponse>((resolve) => {
      releaseOpen = resolve
    })
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(
      hostLink({
        open: () => openGate,
      }),
    )
    render(<App />)
    const open = t("home.open", { title: "th-1" })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: open })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole("button", { name: open }))
    await waitFor(() => {
      expect(screen.getByRole("button", { name: t("thread.back") })).toBeInTheDocument()
    })
    await act(async () => {
      expect(systemBack()).toBe(true)
    })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: open })).toBeInTheDocument()
    })
    await act(async () => {
      releaseOpen({
        v: 1,
        id: "late",
        ok: true,
        detail: { id: "th-1", title: "th-1" },
      })
    })
    expect(screen.queryByRole("button", { name: t("thread.back") })).not.toBeInTheDocument()
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
  })

  it("does not surface a late watch failure after Back", async () => {
    let releaseWatch: (resp: RemoteResponse) => void = () => undefined
    let watchStarted = false
    const watchGate = new Promise<RemoteResponse>((resolve) => {
      releaseWatch = resolve
    })
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(
      hostLink({
        watch: () => {
          watchStarted = true
          return watchGate
        },
      }),
    )
    render(<App />)
    const open = t("home.open", { title: "th-1" })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: open })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole("button", { name: open }))
    await waitFor(() => {
      expect(watchStarted).toBe(true)
    })
    await act(async () => {
      expect(systemBack()).toBe(true)
    })
    await act(async () => {
      releaseWatch({ v: 1, id: "late-watch", ok: false, error: "watch failed" })
    })
    expect(screen.queryByText("watch failed")).not.toBeInTheDocument()
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
  })
})

describe("starting a conversation is its own screen", () => {
  afterEach(() => {
    cleanup()
    vi.mocked(openSaved).mockReset()
  })

  it("picks the PC and the project there, then opens what it started", async () => {
    const asked: Partial<RemoteRequest>[] = []
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(
      hostLink({
        projects: [{ id: "p2", name: "notes" }],
        start: async (req) => {
          asked.push(req)
          return {
            v: 1,
            id: "s",
            ok: true,
            threads: [
              { id: "th-2", title: "th-2", running: true, last_active_at: "2026-01-01T00:00:00Z" },
            ],
          }
        },
      }),
    )
    render(<App />)
    await waitFor(() => {
      expect(screen.getByTestId("new-chat")).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId("new-chat"))
    await waitFor(() => {
      expect(screen.getByRole("radio", { name: "notes" })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole("radio", { name: "notes" }))
    fireEvent.change(screen.getByLabelText(t("home.newMessage")), { target: { value: "go" } })
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: t("home.start") }))
    })
    expect(asked).toEqual([
      expect.objectContaining({ op: OpStart, text: "go", project_id: "p2" }),
    ])
    await waitFor(() => {
      expect(screen.getByRole("button", { name: t("thread.back") })).toBeInTheDocument()
    })
    expect(screen.queryByRole("radio", { name: "notes" })).not.toBeInTheDocument()
  })

  it("a project row opens new chat already on that project", async () => {
    const asked: Partial<RemoteRequest>[] = []
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(
      hostLink({
        projects: [{ id: "p2", name: "notes" }],
        start: async (req) => {
          asked.push(req)
          return {
            v: 1,
            id: "s",
            ok: true,
            threads: [
              { id: "th-2", title: "th-2", running: true, last_active_at: "2026-01-01T00:00:00Z" },
            ],
          }
        },
      }),
    )
    render(<App />)
    const startHere = t("home.newChatIn", { name: "notes" })
    await waitFor(() => {
      expect(screen.getByRole("button", { name: startHere })).toBeInTheDocument()
    })
    fireEvent.click(screen.getByRole("button", { name: startHere }))
    expect(screen.getByRole("radio", { name: "notes" })).toBeChecked()
    fireEvent.change(screen.getByLabelText(t("home.newMessage")), { target: { value: "go" } })
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: t("home.start") }))
    })
    expect(asked).toEqual([
      expect.objectContaining({ op: OpStart, text: "go", project_id: "p2" }),
    ])
  })

  it("system back leaves the new-conversation screen on the inbox", async () => {
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(hostLink())
    render(<App />)
    await waitFor(() => {
      expect(screen.getByTestId("new-chat")).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId("new-chat"))
    expect(screen.getByLabelText(t("home.newMessage"))).toBeInTheDocument()
    let stayed = false
    await act(async () => {
      stayed = systemBack()
    })
    expect(stayed).toBe(true)
    expect(screen.queryByLabelText(t("home.newMessage"))).not.toBeInTheDocument()
    expect(screen.getByRole("tablist", { name: t("home.hosts") })).toBeInTheDocument()
    expect(systemBack()).toBe(false)
  })

  // The typed message only exists in that box, so a refusal must not take
  // the screen away with it.
  it("keeps the screen and says so when the PC refuses the start", async () => {
    seedLink()
    vi.mocked(openSaved).mockResolvedValue(
      hostLink({ start: async () => ({ v: 1, id: "s", ok: false, error: "quota" }) }),
    )
    render(<App />)
    await waitFor(() => {
      expect(screen.getByTestId("new-chat-top")).toBeInTheDocument()
    })
    fireEvent.click(screen.getByTestId("new-chat-top"))
    fireEvent.change(screen.getByLabelText(t("home.newMessage")), { target: { value: "go" } })
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: t("home.start") }))
    })
    await waitFor(() => {
      expect(screen.getByRole("alert").textContent).toBe(t("err.remote", { detail: "quota" }))
    })
    expect(screen.getByLabelText(t("home.newMessage"))).toBeInTheDocument()
  })
})

/** Add a PC is a menu row: the corner it used to share is New chat now. */
function openAddHost() {
  fireEvent.click(screen.getByRole("button", { name: t("home.menu") }))
  fireEvent.click(screen.getByRole("menuitem", { name: t("home.addHost") }))
}

function systemBack(): boolean {
  const fn = window.__zwaiAndroidBack
  if (!fn) throw new Error("android back hook missing")
  return fn()
}

function hostLink(opts?: {
  threads?: { id: string; title: string; running: boolean; last_active_at: string }[]
  running?: { thread_id: string; title: string }[]
  projects?: { id: string; name: string }[]
  list?: () => Promise<RemoteResponse>
  open?: () => Promise<RemoteResponse>
  watch?: () => Promise<RemoteResponse>
  start?: (req: Partial<RemoteRequest>) => Promise<RemoteResponse>
}): DeviceLink {
  const threads = opts?.threads ?? [
    { id: "th-1", title: "th-1", running: false, last_active_at: "2026-01-01T00:00:00Z" },
  ]
  const running = opts?.running ?? []
  const link = {
    path: "relay",
    alive: () => true,
    close: () => undefined,
    announceDevice: async () => undefined,
    rpc: async (req: Partial<RemoteRequest>): Promise<RemoteResponse> => {
      if (req.op === OpHello) return { v: 1, id: "h", ok: true }
      if (req.op === OpStart && opts?.start) return opts.start(req)
      if (req.op === OpList) {
        if (opts?.list) return opts.list()
        return { v: 1, id: "l", ok: true, threads, running, projects: opts?.projects ?? [] }
      }
      if (req.op === OpOpen) {
        if (opts?.open) return opts.open()
        return {
          v: 1,
          id: "o",
          ok: true,
          detail: { id: req.thread_id ?? "th-1", title: "th-1" },
        }
      }
      if (req.op === OpWatch) {
        if (opts?.watch) return opts.watch()
        return { v: 1, id: "w", ok: true, op: "ready", events: [] }
      }
      if (req.op === OpUnwatch) return { v: 1, id: "u", ok: true }
      return { v: 1, id: "x", ok: true }
    },
  }
  return link as unknown as DeviceLink
}
