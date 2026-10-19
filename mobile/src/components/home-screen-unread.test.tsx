import { fireEvent, render, screen, within } from "@testing-library/react"
import type { ComponentProps } from "react"
import { describe, expect, it, vi } from "vitest"

import type { RunningView, ThreadView } from "@/lib/rpc"
import { HomeScreen } from "./home-screen"

type HomeProps = ComponentProps<typeof HomeScreen>

const completed = (patch: Partial<ThreadView> = {}): ThreadView => ({
  id: "task-a",
  title: "Previous result",
  running: false,
  last_active_at: "2026-01-01T00:00:00Z",
  unread: true,
  completed_seq: 17,
  ...patch,
})

function props(patch: Partial<HomeProps> = {}): HomeProps {
  return {
    hosts: [], activeFingerprint: "", projects: [], threads: [], running: [],
    more: false, path: "relay", onOpen: vi.fn(), onMore: vi.fn(),
    onNewChat: vi.fn(), onSelectHost: vi.fn(), onAddHost: vi.fn(), onUnlink: vi.fn(),
    ...patch,
  }
}

const liveCases: {
  name: string
  running?: RunningView[]
  threads?: ThreadView[]
  attention?: ThreadView[]
}[] = [
  { name: "the live roster", running: [{ thread_id: "task-a", title: "Current work", action: "Working" }] },
  { name: "a parked roster wait", running: [{ thread_id: "task-a", title: "Current work", waiting: true }] },
  { name: "a pending question", running: [{ thread_id: "task-a", title: "Current work", ask_user: true }] },
  { name: "the listing running flag", threads: [completed({ running: true })] },
  { name: "the listing waiting flag", threads: [completed({ waiting: true })] },
  { name: "an attention-only running flag", attention: [completed({ running: true })] },
  { name: "an attention-only waiting flag", attention: [completed({ waiting: true })] },
]

describe("HomeScreen completed unread projection", () => {
  // A prior completion stays unread while a newer turn or wait owns this ID.
  // Hiding its old result must not acknowledge or discard that completion.
  it.each(liveCases)("does not label $name as completed unread", ({ running = [], threads = [], attention = [completed()] }) => {
    const input = props({ running, threads, attention })
    const before = structuredClone({ running, threads, attention })
    render(<HomeScreen {...input} />)

    expect(screen.queryByTestId("completion-attention")).not.toBeInTheDocument()
    expect(input.onOpen).not.toHaveBeenCalled()
    expect({ running, threads, attention }).toEqual(before)
  })

  it("retains a different completed ID even when its title matches a live task", () => {
    const input = props({
      running: [{ thread_id: "task-a", title: "Shared title", action: "Working" }],
      attention: [completed({ title: "Shared title" }), completed({ id: "task-b", title: "Shared title" })],
    })
    render(<HomeScreen {...input} />)

    const attention = screen.getByTestId("completion-attention")
    const row = within(attention).getByRole("button", { name: "Open Shared title" })
    fireEvent.click(row)
    expect(input.onOpen).toHaveBeenCalledWith("task-b")
    expect(screen.getByText("In progress")).toBeInTheDocument()
  })

  it("does not revive an active completion when search only matches its old preview", () => {
    render(<HomeScreen {...props({
      running: [{ thread_id: "task-a", title: "Current work", action: "Processing" }],
      attention: [
        completed({ summary: "retained keyword" }),
        completed({ id: "task-b", title: "Idle result", summary: "retained keyword" }),
      ],
    })} />)
    fireEvent.change(screen.getByLabelText("Search conversations"), { target: { value: "retained keyword" } })

    expect(screen.queryByText("In progress")).not.toBeInTheDocument()
    const attention = screen.getByTestId("completion-attention")
    expect(within(attention).queryByRole("button", { name: "Open Previous result" })).not.toBeInTheDocument()
    expect(within(attention).getByRole("button", { name: "Open Idle result" })).toBeInTheDocument()
  })

  it("shows the same unread watermark again after the task becomes idle", () => {
    const row = completed()
    Object.freeze(row)
    const input = props({ attention: [row], running: [{ thread_id: row.id, title: "Current work", waiting: true }] })
    const view = render(<HomeScreen {...input} />)
    expect(screen.queryByTestId("completion-attention")).not.toBeInTheDocument()

    view.rerender(<HomeScreen {...input} running={[]} />)
    const attention = screen.getByTestId("completion-attention")
    expect(within(attention).getByRole("button", { name: "Open Previous result" })).toBeInTheDocument()
    expect(row).toMatchObject({ unread: true, completed_seq: 17 })
    expect(input.onOpen).not.toHaveBeenCalled()
  })

  it("keeps completion More reachable when all loaded completion rows are active", () => {
    const groups = [{ id: "attention", more: true, cursor: "opaque-next" }]
    const input = props({
      running: [{ thread_id: "task-a", title: "Current work" }], attention: [completed()], groups,
    })
    render(<HomeScreen {...input} />)

    const attention = screen.getByTestId("completion-attention")
    expect(within(attention).queryByRole("button", { name: "Open Previous result" })).not.toBeInTheDocument()
    fireEvent.click(within(attention).getByRole("button", { name: "More" }))
    expect(input.onMore).toHaveBeenCalledWith("attention")
    expect(groups).toEqual([{ id: "attention", more: true, cursor: "opaque-next" }])
  })
})
